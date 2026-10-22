// Ensemble Desktop —— 本机桌面壳。
//
// 只做三件事：
//   1. 起一个窗口，加载本地后端的前端；
//   2. 提供原生目录选择器（浏览器拿不到本机绝对路径）；
//   3. 后端没在跑时，拉起同目录的 ensemble 二进制。
//
// 不用 Electron：这里不需要 Node 运行时，WKWebView + AppKit 就够，
// 体积从 ~200MB 降到 ~200KB，启动也更快。

import Cocoa
import WebKit

private let appBase = "http://127.0.0.1:8080"
private let appURL = URL(string: "\(appBase)/app/")!

final class AppDelegate: NSObject, NSApplicationDelegate, WKNavigationDelegate {
    private var window: NSWindow!
    private var webView: WKWebView!
    private var server: Process?
    private var statusItem: NSStatusItem?

    func applicationDidFinishLaunching(_ notification: Notification) {
        buildMenu()
        buildWindow()
        ensureBackendThenLoad()
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        true
    }

    func applicationWillTerminate(_ notification: Notification) {
        if let p = server, p.isRunning {
            p.terminate()
        }
    }

    // MARK: - 窗口

    private func buildWindow() {
        let rect = NSRect(x: 0, y: 0, width: 1320, height: 900)
        window = NSWindow(
            contentRect: rect,
            styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
            backing: .buffered,
            defer: false
        )
        window.title = "合奏 · Ensemble"
        window.minSize = NSSize(width: 980, height: 680)
        window.center()
        window.titlebarAppearsTransparent = false

        let config = WKWebViewConfiguration()
        config.preferences.setValue(true, forKey: "developerExtrasEnabled")
        // 允许页面用 localStorage 保持登录态
        config.websiteDataStore = .default()

        webView = WKWebView(frame: rect, configuration: config)
        webView.navigationDelegate = self
        // 不设 drawsBackground：WKWebView 没有这个 KVC 属性，
        // 用 setValue 会抛 NSUnknownKeyException，进程直接 exit 1。
        webView.underPageBackgroundColor = .windowBackgroundColor
        window.contentView = webView
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    private func buildMenu() {
        let main = NSMenu()

        let appItem = NSMenuItem()
        let appMenu = NSMenu()
        appMenu.addItem(withTitle: "关于 Ensemble", action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)), keyEquivalent: "")
        appMenu.addItem(.separator())
        appMenu.addItem(withTitle: "重新载入", action: #selector(reload), keyEquivalent: "r")
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

        let editItem = NSMenuItem()
        let editMenu = NSMenu(title: "编辑")
        editMenu.addItem(withTitle: "复制", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        editMenu.addItem(withTitle: "粘贴", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        editMenu.addItem(withTitle: "全选", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        editItem.submenu = editMenu
        main.addItem(editItem)

        NSApp.mainMenu = main
    }

    // MARK: - 后端

    /// 8080 没响应时，尝试拉起同目录的 ensemble 二进制。
    private func ensureBackendThenLoad() {
        // 启动瞬间网络栈可能还没就绪，ping 一次失败不代表服务没在跑。
        // 重试三次再决定拉起自己的后端，否则会和已经跑着的实例抢 8080。
        for _ in 0..<3 {
            if ping() {
                loadApp()
                return
            }
            Thread.sleep(forTimeInterval: 0.4)
        }
        startBackend()
        // 轮询到起来为止（最多 12 秒），期间显示加载页
        var tries = 0
        DispatchQueue.global().async { [weak self] in
            while tries < 120 {
                Thread.sleep(forTimeInterval: 0.1)
                tries += 1
                if self?.ping() == true {
                    DispatchQueue.main.async { self?.loadApp() }
                    return
                }
            }
            DispatchQueue.main.async { self?.loadApp() } // 起来失败也加载，让页面自己报错
        }
    }

    private func ping() -> Bool {
        guard let url = URL(string: "\(appBase)/healthz") else { return false }
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

    private func startBackend() {
        let fm = FileManager.default
        let execDir = Bundle.main.bundleURL.deletingLastPathComponent()
        let candidates = [
            execDir.appendingPathComponent("ensemble"),
            execDir.appendingPathComponent("bin/ensemble"),
            URL(fileURLWithPath: fm.currentDirectoryPath).appendingPathComponent("bin/ensemble"),
            URL(fileURLWithPath: fm.currentDirectoryPath).appendingPathComponent("ensemble"),
        ]
        guard let bin = candidates.first(where: { fm.isExecutableFile(atPath: $0.path) }) else {
            NSLog("找不到 ensemble 二进制，请先用 make build 构建，或手动启动服务")
            return
        }
        let p = Process()
        p.executableURL = bin
        p.currentDirectoryURL = bin.deletingLastPathComponent().deletingLastPathComponent()
        // 继承当前环境（含 DEEPSEEK_API_KEY），不额外注入
        p.environment = ProcessInfo.processInfo.environment
        do {
            try p.run()
            server = p
            NSLog("已启动本地后端：\(bin.path) pid=\(p.processIdentifier)")
        } catch {
            NSLog("启动后端失败：\(error)")
        }
    }

    private func loadApp() {
        webView.load(URLRequest(url: appURL))
    }

    @objc private func reload() {
        webView.reload()
    }

    // MARK: - 原生目录选择

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
            let path = url.path
            let escaped = path.replacingOccurrences(of: "\\", with: "\\\\")
                .replacingOccurrences(of: "'", with: "\\'")
            let js = "window.__ensemblePickDirectory && window.__ensemblePickDirectory('\(escaped)')"
            self?.webView.evaluateJavaScript(js) { _, err in
                if let err = err {
                    NSLog("注入目录失败：\(err)")
                }
            }
        }
    }

    // MARK: - WKNavigationDelegate

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        window.title = "合奏 · Ensemble"
    }
}

// 未捕获的 ObjC 异常会直接让进程消失，日志里什么都不留。
// 挂一个处理器，至少能看到崩在哪。
NSSetUncaughtExceptionHandler { exc in
    let msg = "UNCAUGHT \(exc.name.rawValue): \(exc.reason ?? "?")\n\(exc.callStackSymbols.joined(separator: "\n"))\n"
    FileHandle.standardError.write(msg.data(using: .utf8) ?? Data())
}

let app = NSApplication.shared
app.setActivationPolicy(.regular)
let delegate = AppDelegate()
app.delegate = delegate
app.run()
