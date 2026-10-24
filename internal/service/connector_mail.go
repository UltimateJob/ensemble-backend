package service

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// QQ 邮箱「最近邮件」读取。
//
// 只做最小子集：SELECT INBOX + FETCH 最近 N 封的头部。
// 不解析 MIME 结构、不下载正文附件——那是一个完整的邮件库该做的事，
// 而 Agent 需要的是「最近收到什么」，用于摘要和待办提取。
//
// 这里手写 IMAP 命令行而不是引第三方库：目前只用到 4 个命令
// （LOGIN / SELECT / FETCH / LOGOUT），加一个依赖的维护成本比它省下的
// 代码更多。

const imapHost = "imap.qq.com:993"

// fetchRecentMail 返回最近 N 封邮件的摘要文本。
func fetchRecentMail(ctx context.Context, addr, authCode string, limit int) (string, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", imapHost, &tls.Config{ServerName: "imap.qq.com"})
	if err != nil {
		return "", fmt.Errorf("连不上 QQ 邮箱 IMAP：%v", err)
	}
	defer conn.Close()

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(25 * time.Second)
	}
	_ = conn.SetDeadline(deadline)

	if line, err := readIMAPLine(conn); err != nil || !strings.HasPrefix(line, "* OK") {
		return "", errors.New("IMAP 握手失败")
	}

	// LOGIN
	if _, err := fmt.Fprintf(conn, "a1 LOGIN %q %q\r\n", addr, authCode); err != nil {
		return "", err
	}
	if err := waitIMAPOK(conn, "a1"); err != nil {
		return "", errors.New("邮箱拒绝了这组凭据（检查授权码）")
	}

	// SELECT INBOX，拿邮件总数
	if _, err := fmt.Fprintf(conn, "a2 SELECT INBOX\r\n"); err != nil {
		return "", err
	}
	total, err := selectMailbox(conn, "a2")
	if err != nil {
		return "", err
	}
	if total == 0 {
		return "收件箱是空的。", nil
	}

	from := total - limit + 1
	if from < 1 {
		from = 1
	}
	// 只取头部字段，不取正文：正文可能是几十 KB 的 HTML
	cmd := fmt.Sprintf("a3 FETCH %d:%d (BODY.PEEK[HEADER.FIELDS (FROM SUBJECT DATE)])\r\n", from, total)
	if _, err := fmt.Fprint(conn, cmd); err != nil {
		return "", err
	}
	headers, err := readFetchHeaders(conn, "a3")
	if err != nil {
		return "", err
	}

	_, _ = fmt.Fprint(conn, "a4 LOGOUT\r\n")

	msgs := parseMailHeaders(headers)
	if len(msgs) == 0 {
		return "没有读到邮件头部。", nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "最近 %d 封邮件（共 %d 封）：\n\n", len(msgs), total)
	for i := len(msgs) - 1; i >= 0; i-- { // 新的在前
		m := msgs[i]
		fmt.Fprintf(&sb, "- 主题：%s\n  发件人：%s\n  时间：%s\n\n",
			orDash(m["subject"]), orDash(m["from"]), orDash(m["date"]))
	}
	return sb.String(), nil
}

func orDash(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "（无）"
	}
	return decodeMIMEHeader(s)
}

// waitIMAPOK 读到 tagged OK 为止。NO/BAD 报错。
func waitIMAPOK(conn net.Conn, tag string) error {
	for i := 0; i < 12; i++ {
		line, err := readIMAPLine(conn)
		if err != nil {
			return err
		}
		switch {
		case strings.HasPrefix(line, tag+" OK"):
			return nil
		case strings.HasPrefix(line, tag+" NO"), strings.HasPrefix(line, tag+" BAD"):
			return errors.New(line)
		}
	}
	return errors.New("IMAP 登录超时")
}

// selectMailbox 执行 SELECT 并解析 "* N EXISTS"。
func selectMailbox(conn net.Conn, tag string) (int, error) {
	total := 0
	for i := 0; i < 30; i++ {
		line, err := readIMAPLine(conn)
		if err != nil {
			return 0, err
		}
		var n int
		if _, err := fmt.Sscanf(line, "* %d EXISTS", &n); err == nil {
			total = n
		}
		if strings.HasPrefix(line, tag+" OK") {
			return total, nil
		}
		if strings.HasPrefix(line, tag+" NO") || strings.HasPrefix(line, tag+" BAD") {
			return 0, errors.New("无法打开收件箱")
		}
	}
	return total, nil
}

// readFetchHeaders 收 FETCH 响应里 HEADER 与结束符。
// 返回每封邮件的头部原文，按出现顺序。
func readFetchHeaders(conn net.Conn, tag string) ([]string, error) {
	var out []string
	var cur []string
	inHeader := false

	for i := 0; i < 2000; i++ {
		line, err := readIMAPLine(conn)
		if err != nil {
			return out, err
		}
		// FETCH 响应形如：* 42 FETCH (UID 123 BODY[HEADER.FIELDS ...] {N}
		if strings.HasPrefix(line, "* ") && strings.Contains(line, "FETCH") {
			inHeader = true
			cur = nil
			continue
		}
		if strings.HasPrefix(line, tag+" OK") {
			if len(cur) > 0 {
				out = append(out, strings.Join(cur, "\n"))
			}
			return out, nil
		}
		if strings.HasPrefix(line, tag+" NO") || strings.HasPrefix(line, tag+" BAD") {
			return out, errors.New("读取邮件失败")
		}
		if !inHeader {
			continue
		}
		// 头部结束标记：单独一个 ")"
		if strings.TrimSpace(line) == ")" {
			if len(cur) > 0 {
				out = append(out, strings.Join(cur, "\n"))
			}
			cur = nil
			inHeader = false
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		cur = append(cur, line)
	}
	return out, nil
}

// parseMailHeaders 把 "From: xxx" 这类块解析成 map。
func parseMailHeaders(blocks []string) []map[string]string {
	var out []map[string]string
	for _, blk := range blocks {
		m := map[string]string{}
		var lastKey string
		for _, line := range strings.Split(blk, "\n") {
			// 折行：以空白开头表示续上一行
			if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
				if lastKey != "" {
					m[lastKey] += " " + strings.TrimSpace(line)
				}
				continue
			}
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			lastKey = strings.ToLower(strings.TrimSpace(k))
			m[lastKey] = strings.TrimSpace(v)
		}
		if len(m) > 0 {
			out = append(out, m)
		}
	}
	return out
}

// decodeMIMEHeader 处理 RFC2047 的编码字（=?utf-8?B?...?=）。
// QQ 邮箱的中文主题基本都是这种形式，不解码就是一堆乱码。
func decodeMIMEHeader(s string) string {
	if !strings.Contains(s, "=?") {
		return s
	}
	var sb strings.Builder
	rest := s
	for {
		i := strings.Index(rest, "=?")
		if i < 0 {
			sb.WriteString(rest)
			break
		}
		sb.WriteString(rest[:i])
		end := strings.Index(rest[i:], "?=")
		if end < 0 {
			sb.WriteString(rest[i:])
			break
		}
		chunk := rest[i : i+end+2]
		rest = rest[i+end+2:]
		dec, ok := decodeEncodedWord(chunk)
		if ok {
			sb.WriteString(dec)
		}
	}
	return sb.String()
}

func decodeEncodedWord(w string) (string, bool) {
	// =?charset?B?base64?=  /  =?charset?Q?quoted?=
	body := strings.TrimSuffix(strings.TrimPrefix(w, "=?"), "?=")
	parts := strings.SplitN(body, "?", 3)
	if len(parts) != 3 {
		return "", false
	}
	enc, data := strings.ToUpper(parts[1]), parts[2]
	switch enc {
	case "B":
		b, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return "", false
		}
		return string(b), true
	case "Q":
		data = strings.ReplaceAll(data, "_", " ")
		b, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return data, true
		}
		return string(b), true
	}
	return "", false
}
