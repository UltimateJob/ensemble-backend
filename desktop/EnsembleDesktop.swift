// 纸团桌面壳 —— 原生窗口 + 后端自启 + 原生目录选择 + 下载 + 通知。
//
// 与上一版的区别（都是实测踩出来的）：
//   1. 后端二进制在 .app 包内（Contents/MacOS/ensemble），不是 .app 的上级目录；
//   2. 静态资源在 Contents/Resources/web/，由 Go 的 webDir 第三条路径命中；
//   3. 端口不再写死 8080：探测 8080–8099，把选中的端口通过 ADDR 传给子进程；
//   4. 配置按「应用支持目录 → 仓库根」顺序读 .env，Finder 启动也能拿到 Key；
//   5. 数据默认写到 ~/Library/Application Support/纸团/data，不放进 .app 内部。
//
// 不用 Electron：这里不需要 Node 运行时，WKWebView + AppKit 就够。

import Cocoa
import WebKit
import UserNotifications

private let appFolder = "纸团"

private let supportDir: URL = {
    let base = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
    return base.appendingPathComponent(appFolder, isDirectory: true)
}()

private var serverProcess: Process?

final class AppDelegate: NSObject, NSApplicationDelegate, WKNavigationDelegate,
                         WKUIDelegate, WKDownloadDelegate, WKScriptMessageHandler {
    private var window: NSWindow!
    private var webView: WKWebView!
    private var baseURL = "http://127.0.0.1:8080"
    private var didShowError = false

    func applicationDidFinishLaunching(_ notification: Notification) {
        try? FileManager.default.createDirectory(at: supportDir, withIntermediateDirectories: true)
        buildMenu()
        buildWindow()
        requestNotifyPermission()
        startFlow()
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }

    func applicationWillTerminate(_ notification: Notification) {
        if let p = serverProcess, p.isRunning { p.terminate() }
    }

    // MARK: - 窗口

    private func buildWindow() {
        let cfg = WKWebViewConfiguration()
        cfg.preferences.setValue(true, forKey: "developerExtrasEnabled")
        cfg.websiteDataStore = .default()
        cfg.userContentController.add(self, name: "notify")

        webView = WKWebView(frame: .zero, configuration: cfg)
        webView.navigationDelegate = self
        webView.uiDelegate = self
        webView.underPageBackgroundColor = .white

        window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 1320, height: 900),
            styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
            backing: .buffered,
            defer: false
        )
        window.title = "纸团"
        window.minSize = NSSize(width: 1080, height: 720)
        window.contentView = webView
        window.setFrameAutosaveName("ZhituanMainWindow")
        window.center()
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    private func buildMenu() {
        let main = NSMenu()

        let appItem = NSMenuItem()
        let appMenu = NSMenu()
        appMenu.addItem(withTitle: "关于纸团",
                        action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)),
                        keyEquivalent: "")
        appMenu.addItem(.separator())
        appMenu.addItem(withTitle: "重新载入", action: #selector(reload), keyEquivalent: "r")
        appMenu.addItem(withTitle: "打开日志", action: #selector(openLog), keyEquivalent: "l")
        appMenu.addItem(.separator())
        appMenu.addItem(withTitle: "退出", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        appItem.submenu = appMenu
        main.addItem(appItem)

        let fileItem = NSMenuItem()
        let fileMenu = NSMenu(title: "文件")
        let pick = NSMenuItem(title: "选择工作区目录…", action: #selector(pickWorkspace), keyEquivalent: "o")
        pick.target = self
        fileMenu.addItem(pick)
        fileItem.submenu = fileMenu
        main.addItem(fileItem)

        let goItem = NSMenuItem()
        let goMenu = NSMenu(title: "前往")
        let names = ["团队大厅", "聊天房间", "目标看板", "连接器", "资料库", "设置"]
        for (i, name) in names.enumerated() {
            let item = NSMenuItem(title: name, action: #selector(goTo(_:)), keyEquivalent: "\(i + 1)")
            item.tag = i
            item.target = self
            goMenu.addItem(item)
        }
        goMenu.addItem(.separator())
        let panel = NSMenuItem(title: "切换工作面板", action: #selector(togglePanel), keyEquivalent: "0")
        panel.target = self
        goMenu.addItem(panel)
        goItem.submenu = goMenu
        main.addItem(goItem)

        let viewItem = NSMenuItem()
        let viewMenu = NSMenu(title: "显示")
        viewMenu.addItem(withTitle: "放大", action: #selector(NSWindow.performZoom(_:)), keyEquivalent: "")
        viewMenu.addItem(.separator())
        let dev = NSMenuItem(title: "开发者工具", action: #selector(openDevTools), keyEquivalent: "i")
        dev.target = self
        viewMenu.addItem(dev)
        viewItem.submenu = viewMenu
        main.addItem(viewItem)

        let editItem = NSMenuItem()
        let editMenu = NSMenu(title: "编辑")
        editMenu.addItem(withTitle: "复制", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        editMenu.addItem(withTitle: "粘贴", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        editMenu.addItem(withTitle: "全选", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        editItem.submenu = editMenu
        main.addItem(editItem)

        NSApp.mainMenu = main
    }

    // MARK: - 启动流程

    private func startFlow() {
        // 已经有一个后端在跑（比如用户先跑了 scripts/dev.sh）就直接用。
        for _ in 0..<3 {
            if ping(baseURL) {
                loadApp()
                return
            }
            Thread.sleep(forTimeInterval: 0.35)
        }
        guard let port = freePort(from: 8080, through: 8099) else {
            showError("没有可用端口（8080–8099 都被占用了）。关掉一个再试。")
            return
        }
        baseURL = "http://127.0.0.1:\(port)"
        startBackend(port: port)
        pollUntilReady(tries: 150)
    }

    private func ping(_ base: String) -> Bool {
        guard let url = URL(string: base + "/healthz") else { return false }
        var req = URLRequest(url: url)
        req.timeoutInterval = 0.6
        let sem = DispatchSemaphore(value: 0)
        var alive = false
        URLSession.shared.dataTask(with: req) { _, resp, _ in
            alive = (resp as? HTTPURLResponse)?.statusCode == 200
            sem.signal()
        }.resume()
        _ = sem.wait(timeout: .now() + 1.2)
        return alive
    }

    private func pollUntilReady(tries: Int) {
        DispatchQueue.global().async { [weak self] in
            guard let self else { return }
            for i in 0..<tries {
                Thread.sleep(forTimeInterval: 0.1)
                // 子进程提前退出：不用等满 15 秒，直接把日志位置告诉用户。
                if i > 20, let p = serverProcess, !p.isRunning {
                    DispatchQueue.main.async {
                        self.showError("后端启动失败。\n日志：\(supportDir.path)/ensemble.log\n常见原因：端口被占用、数据目录不可写。")
                    }
                    return
                }
                if self.ping(self.baseURL) {
                    DispatchQueue.main.async { self.loadApp() }
                    return
                }
            }
            DispatchQueue.main.async {
                self.showError("后端 15 秒内没有就绪。\n日志：\(supportDir.path)/ensemble.log")
            }
        }
    }

    private func startBackend(port: Int) {
        // 二进制在包内：Contents/MacOS/ensemble，与壳可执行文件同级。
        guard let exeDir = Bundle.main.executableURL?.deletingLastPathComponent() else {
            showError("找不到应用可执行文件位置。")
            return
        }
        let candidates = [
            exeDir.appendingPathComponent("ensemble"),
            Bundle.main.bundleURL.appendingPathComponent("Contents/Resources/ensemble"),
        ]
        guard let bin = candidates.first(where: { FileManager.default.isExecutableFile(atPath: $0.path) }) else {
            showError("应用包内缺少 ensemble 二进制。\n请重新运行 desktop/build.sh 打包。")
            return
        }

        var env = ProcessInfo.processInfo.environment
        for (k, v) in loadDotEnv() { env[k] = v }
        env["ADDR"] = ":\(port)"
        if env["DATA_DIR"] == nil {
            env["DATA_DIR"] = supportDir.appendingPathComponent("data").path
        }

        let p = Process()
        p.executableURL = bin
        p.environment = env
        p.currentDirectoryURL = Bundle.main.bundleURL

        let logURL = supportDir.appendingPathComponent("ensemble.log")
        FileManager.default.createFile(atPath: logURL.path, contents: nil)
        if let handle = try? FileHandle(forWritingTo: logURL) {
            _ = try? handle.seekToEnd()
            p.standardOutput = handle
            p.standardError = handle
        }
        do {
            try p.run()
            serverProcess = p
        } catch {
            showError("启动后端失败：\(error.localizedDescription)")
        }
    }

    /// 依次读应用支持目录与仓库根的 .env，先读到的优先。
    private func loadDotEnv() -> [String: String] {
        var out: [String: String] = [:]
        var candidates = [supportDir.appendingPathComponent(".env")]
        if let cwd = ProcessInfo.processInfo.environment["PWD"] {
            candidates.append(URL(fileURLWithPath: cwd).appendingPathComponent(".env"))
        }
        for url in candidates {
            guard let text = try? String(contentsOf: url, encoding: .utf8) else { continue }
            for line in text.split(separator: "\n") {
                let t = line.trimmingCharacters(in: .whitespaces)
                if t.isEmpty || t.hasPrefix("#") { continue }
                guard let eq = t.firstIndex(of: "=") else { continue }
                let key = String(t[..<eq]).trimmingCharacters(in: .whitespaces)
                var value = String(t[t.index(after: eq)...]).trimmingCharacters(in: .whitespaces)
                if value.count >= 2, value.hasPrefix("\""), value.hasSuffix("\"") {
                    value = String(value.dropFirst().dropLast())
                }
                if !key.isEmpty, out[key] == nil { out[key] = value }
            }
        }
        return out
    }

    private func freePort(from: Int, through: Int) -> Int? {
        for port in from...through {
            let fd = socket(AF_INET, SOCK_STREAM, 0)
            if fd < 0 { continue }
            defer { close(fd) }
            var addr = sockaddr_in()
            addr.sin_family = sa_family_t(AF_INET)
            addr.sin_port = in_port_t(UInt16(port).bigEndian)
            addr.sin_addr.s_addr = inet_addr("127.0.0.1")
            let bound = withUnsafePointer(to: &addr) { ptr in
                ptr.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                    Darwin.bind(fd, $0, socklen_t(MemoryLayout<sockaddr_in>.size))
                }
            }
            if bound == 0 { return port }
        }
        return nil
    }

    private func loadApp() {
        webView.load(URLRequest(url: URL(string: baseURL + "/app/")!))
    }

    private func showError(_ message: String) {
        guard !didShowError else { return }
        didShowError = true
        let html = """
        <html><head><meta charset="utf-8"></head>
        <body style="font:15px -apple-system,BlinkMacSystemFont,'PingFang SC';background:#fff;color:#1A1A1D;
                     display:flex;align-items:center;justify-content:center;height:100vh;margin:0">
        <div style="max-width:520px;text-align:center;padding:24px">
          <h2 style="font-size:20px;font-weight:600;margin:0 0 12px">打不开纸团</h2>
          <p style="color:#5A5A63;line-height:1.75;white-space:pre-wrap;text-align:left">\(message)</p>
          <button onclick="location.reload()"
                  style="margin-top:22px;padding:10px 22px;border:0;border-radius:12px;
                         background:#367BED;color:#fff;font-size:14px;cursor:pointer">重试</button>
        </div></body></html>
        """
        webView.loadHTMLString(html, baseURL: nil)
    }

    // MARK: - 菜单动作

    @objc private func reload() {
        didShowError = false
        if webView.url == nil || webView.url?.path == nil {
            startFlow()
        } else {
            webView.reload()
        }
    }

    @objc private func openLog() {
        let log = supportDir.appendingPathComponent("ensemble.log")
        if FileManager.default.fileExists(atPath: log.path) {
            NSWorkspace.shared.open(log)
        } else {
            NSWorkspace.shared.open(supportDir)
        }
    }

    @objc private func openDevTools() {
        webView.configuration.preferences.setValue(true, forKey: "developerExtrasEnabled")
        webView.evaluateJavaScript("console.log('devtools enabled')", completionHandler: nil)
    }

    @objc private func goTo(_ item: NSMenuItem) {
        let routes = ["lobby", "room", "goals", "connectors", "library", "settings"]
        guard item.tag >= 0, item.tag < routes.count else { return }
        webView.evaluateJavaScript("location.hash = '#/\(routes[item.tag])'", completionHandler: nil)
    }

    @objc private func togglePanel() {
        webView.evaluateJavaScript(
            "document.dispatchEvent(new KeyboardEvent('keydown',{key:'0',metaKey:true}))",
            completionHandler: nil)
    }

    /// 选完目录后注入页面：前端监听到后填进工作区路径输入框。
    @objc private func pickWorkspace() {
        let panel = NSOpenPanel()
        panel.canChooseFiles = false
        panel.canChooseDirectories = true
        panel.allowsMultipleSelection = false
        panel.canCreateDirectories = true
        panel.prompt = "使用这个目录"
        panel.message = "选择代码工作室的工作区（本机绝对路径，不会上传）"
        panel.begin { [weak self] resp in
            guard resp == .OK, let url = panel.url else { return }
            let escaped = url.path
                .replacingOccurrences(of: "\\", with: "\\\\")
                .replacingOccurrences(of: "'", with: "\\'")
            self?.webView.evaluateJavaScript(
                "window.__ensemblePickDirectory && window.__ensemblePickDirectory('\(escaped)')",
                completionHandler: nil)
        }
    }

    // MARK: - WKNavigationDelegate

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        window.title = "纸团"
    }

    /// 外链走系统浏览器，站内跳转留在壳里。
    func webView(_ webView: WKWebView, decidePolicyFor navigationAction: WKNavigationAction,
                 decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        if navigationAction.navigationType == .linkActivated,
           let url = navigationAction.request.url,
           let host = url.host, !host.hasPrefix("127.0.0.1"), host != "localhost" {
            NSWorkspace.shared.open(url)
            decisionHandler(.cancel)
            return
        }
        decisionHandler(.allow)
    }

    func webView(_ webView: WKWebView, navigationAction: WKNavigationAction, didBecome download: WKDownload) {
        download.delegate = self
    }

    // MARK: - WKUIDelegate

    func webView(_ webView: WKWebView, runJavaScriptAlertPanelWithMessage message: String,
                 initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping () -> Void) {
        let alert = NSAlert()
        alert.messageText = message
        alert.addButton(withTitle: "好")
        alert.runModal()
        completionHandler()
    }

    func webView(_ webView: WKWebView, runJavaScriptConfirmPanelWithMessage message: String,
                 initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping (Bool) -> Void) {
        let alert = NSAlert()
        alert.messageText = message
        alert.addButton(withTitle: "确定")
        alert.addButton(withTitle: "取消")
        completionHandler(alert.runModal() == .alertFirstButtonReturn)
    }

    // MARK: - WKDownloadDelegate

    func download(_ download: WKDownload, decideDestinationUsing response: URLResponse,
                  suggestedFilename: String, completionHandler: @escaping (URL?) -> Void) {
        let dir = FileManager.default.urls(for: .downloadsDirectory, in: .userDomainMask).first
            ?? FileManager.default.homeDirectoryForCurrentUser
        var target = dir.appendingPathComponent(suggestedFilename)
        // 同名文件不覆盖：加 (1)(2) 后缀，和浏览器行为一致。
        var n = 1
        let base = target.deletingPathExtension().lastPathComponent
        let ext = target.pathExtension
        while FileManager.default.fileExists(atPath: target.path) {
            let name = ext.isEmpty ? "\(base) (\(n))" : "\(base) (\(n)).\(ext)"
            target = dir.appendingPathComponent(name)
            n += 1
        }
        completionHandler(target)
    }

    func downloadDidFinish(_ download: WKDownload) {
        NSSound(named: "Glass")?.play()
    }

    // MARK: - 通知

    func userContentController(_ userContentController: WKUserContentController,
                               didReceive message: WKScriptMessage) {
        guard message.name == "notify",
              let body = message.body as? [String: Any] else { return }
        let content = UNMutableNotificationContent()
        content.title = body["title"] as? String ?? "纸团"
        content.body = body["body"] as? String ?? ""
        let req = UNNotificationRequest(identifier: UUID().uuidString, content: content, trigger: nil)
        UNUserNotificationCenter.current().add(req)
    }

    private func requestNotifyPermission() {
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound]) { _, _ in }
    }
}

// 未捕获的 ObjC 异常会直接让进程消失，日志里什么都不留。
NSSetUncaughtExceptionHandler { exc in
    let msg = "UNCAUGHT \(exc.name.rawValue): \(exc.reason ?? "?")\n\(exc.callStackSymbols.joined(separator: "\n"))\n"
    FileHandle.standardError.write(msg.data(using: .utf8) ?? Data())
}

let app = NSApplication.shared
app.setActivationPolicy(.regular)
let delegate = AppDelegate()
app.delegate = delegate
app.run()
