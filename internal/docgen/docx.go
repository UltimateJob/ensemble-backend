// Package docgen 用标准库生成 Office 文档（docx / xlsx）。
//
// 为什么不用 python-docx / openpyxl：本项目是零第三方依赖的单二进制部署，
// 生成 docx 本质是「zip + 几段 XML」，标准库足够。
//
// 范围刻意做小：标题、段落、要点列表、表格。够写报告、周报、清单，
// 不做样式系统、页眉页脚、图表——那些应该换真正的排版工具。
package docgen

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// Section 文档里的一个章节。
type Section struct {
	Heading    string   `json:"heading"`
	Paragraphs []string `json:"paragraphs,omitempty"`
	Bullets    []string `json:"bullets,omitempty"`
}

// Doc 一份简单的 docx。
type Doc struct {
	Title    string    `json:"title"`
	Subtitle string    `json:"subtitle,omitempty"`
	Sections []Section `json:"sections"`
}

// WriteDocx 把 Doc 写成 docx（OOXML）到 w。
func WriteDocx(w io.Writer, d Doc) error {
	zw := zip.NewWriter(w)
	defer zw.Close()

	files := []struct {
		name string
		body string
	}{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", rootRels},
		{"word/document.xml", documentXML(d)},
		{"docProps/core.xml", coreProps},
	}
	for _, f := range files {
		fw, err := zw.Create(f.name)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(fw, f.body); err != nil {
			return err
		}
	}
	return nil
}

const contentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>
</Types>`

const rootRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>
</Relationships>`

const coreProps = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties"
 xmlns:dc="http://purl.org/dc/elements/1.1/">
<dc:creator>ensemble</dc:creator>
<cp:lastModifiedBy>ensemble</cp:lastModifiedBy>
</cp:coreProperties>`

// documentXML 拼出正文。样式只用内联的 bold/size，避免引入 styles.xml。
func documentXML(d Doc) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	b.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)

	if t := strings.TrimSpace(d.Title); t != "" {
		b.WriteString(para(t, 32, true))
	}
	if s := strings.TrimSpace(d.Subtitle); s != "" {
		b.WriteString(para(s, 22, false))
	}
	for _, sec := range d.Sections {
		if h := strings.TrimSpace(sec.Heading); h != "" {
			b.WriteString(para(h, 26, true))
		}
		for _, p := range sec.Paragraphs {
			if strings.TrimSpace(p) == "" {
				continue
			}
			b.WriteString(para(p, 21, false))
		}
		for _, item := range sec.Bullets {
			if strings.TrimSpace(item) == "" {
				continue
			}
			b.WriteString(para("• "+item, 21, false))
		}
	}

	b.WriteString(`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/>` +
		`<w:pgMar w:top="1134" w:right="1134" w:bottom="1134" w:left="1134"/></w:sectPr>`)
	b.WriteString(`</w:body></w:document>`)
	return b.String()
}

// para 写一段。size 是半磅（21 ≈ 10.5pt，32 ≈ 16pt）。
func para(text string, size int, bold bool) string {
	runProps := fmt.Sprintf(`<w:rPr><w:sz w:val="%d"/><w:szCs w:val="%d"/>`, size, size)
	if bold {
		runProps += `<w:b/>`
	}
	runProps += `</w:rPr>`
	return `<w:p><w:pPr><w:spacing w:after="120"/></w:pPr><w:r>` +
		runProps + `<w:t xml:space="preserve">` + xmlEscape(text) + `</w:t></w:r></w:p>`
}

func xmlEscape(s string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return s
	}
	return b.String()
}

// Table 一张表。
type Table struct {
	Header []string   `json:"header"`
	Rows   [][]string `json:"rows"`
}

// Sheet 一个工作表。
type Sheet struct {
	Name   string     `json:"name"`
	Header []string   `json:"header"`
	Rows   [][]string `json:"rows"`
	Widths []float64  `json:"widths,omitempty"`
}
