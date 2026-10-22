package docgen

import (
	"archive/zip"
	"fmt"
	"io"
	"strings"
)

// WriteXlsx 生成一个多工作表的 xlsx。
//
// 用 inlineStr 而不是 sharedStrings：少一个部件、少一层索引，
// 代价是重复字符串会占更多字节——对这种「几十行报表」的量级无所谓。
func WriteXlsx(w io.Writer, sheets []Sheet) error {
	if len(sheets) == 0 {
		return fmt.Errorf("docgen: 至少需要一个工作表")
	}
	zw := zip.NewWriter(w)
	defer zw.Close()

	parts := []struct{ name, body string }{
		{"[Content_Types].xml", xlsxContentTypes(len(sheets))},
		{"_rels/.rels", xlsxRootRels},
		{"xl/workbook.xml", xlsxWorkbook(sheets)},
		{"xl/_rels/workbook.xml.rels", xlsxWorkbookRels(len(sheets))},
	}
	for i := range sheets {
		parts = append(parts, struct{ name, body string }{
			fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), xlsxSheet(sheets[i]),
		})
	}
	for _, p := range parts {
		fw, err := zw.Create(p.name)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(fw, p.body); err != nil {
			return err
		}
	}
	return nil
}

func xlsxContentTypes(n int) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
		`<Default Extension="xml" ContentType="application/xml"/>` +
		`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>`)
	for i := 1; i <= n; i++ {
		b.WriteString(fmt.Sprintf(`<Override PartName="/xl/worksheets/sheet%d.xml" `+
			`ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, i))
	}
	b.WriteString(`</Types>`)
	return b.String()
}

const xlsxRootRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`

func xlsxWorkbook(sheets []Sheet) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" ` +
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`)
	for i, s := range sheets {
		name := s.Name
		if strings.TrimSpace(name) == "" {
			name = fmt.Sprintf("Sheet%d", i+1)
		}
		b.WriteString(fmt.Sprintf(`<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, xmlEscape(clipName(name)), i+1, i+1))
	}
	b.WriteString(`</sheets></workbook>`)
	return b.String()
}

func xlsxWorkbookRels(n int) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i := 1; i <= n; i++ {
		b.WriteString(fmt.Sprintf(`<Relationship Id="rId%d" `+
			`Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" `+
			`Target="worksheets/sheet%d.xml"/>`, i, i))
	}
	b.WriteString(`</Relationships>`)
	return b.String()
}

func xlsxSheet(s Sheet) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)

	widths := s.Widths
	if len(widths) == 0 && len(s.Header) > 0 {
		widths = make([]float64, len(s.Header))
		for i := range widths {
			widths[i] = 18
		}
	}
	if len(widths) > 0 {
		b.WriteString(`<cols>`)
		for i, w := range widths {
			b.WriteString(fmt.Sprintf(`<col min="%d" max="%d" width="%.1f" customWidth="1"/>`, i+1, i+1, w))
		}
		b.WriteString(`</cols>`)
	}

	b.WriteString(`<sheetData>`)
	rowNum := 1
	if len(s.Header) > 0 {
		b.WriteString(rowXML(rowNum, s.Header))
		rowNum++
	}
	for _, r := range s.Rows {
		b.WriteString(rowXML(rowNum, r))
		rowNum++
	}
	b.WriteString(`</sheetData></worksheet>`)
	return b.String()
}

func rowXML(n int, cells []string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<row r="%d">`, n))
	for i, v := range cells {
		if strings.TrimSpace(v) == "" {
			continue
		}
		ref := fmt.Sprintf("%s%d", colName(i), n)
		b.WriteString(fmt.Sprintf(`<c r="%s" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`,
			ref, xmlEscape(v)))
	}
	b.WriteString(`</row>`)
	return b.String()
}

// colName 把 0 基列号转成 A / Z / AA。
func colName(i int) string {
	name := ""
	for i >= 0 {
		name = string(rune('A'+i%26)) + name
		i = i/26 - 1
	}
	return name
}

// clipName 工作表名上限 31 字符，且不能含 : \ / ? * [ ]
func clipName(s string) string {
	if len(s) > 31 {
		s = s[:31]
	}
	repl := strings.NewReplacer(":", "_", "\\", "_", "/", "_", "?", "_", "*", "_", "[", "(", "]", ")")
	return repl.Replace(s)
}
