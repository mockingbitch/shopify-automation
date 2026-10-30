package products

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

// Import limits (assumption A-7 and risk R7).
const (
	MaxImportBytes = 50 << 20
	MaxImportRows  = 100_000
	maxImportCols  = 60
	maxCellLen     = 10_000
	xlsxUnzipLimit = 200 << 20 // whole archive, uncompressed
	xlsxXMLLimit   = 64 << 20  // one worksheet XML
	maxImageURLs   = 20
)

// Columns of the import file. Unknown columns are ignored.
var Columns = []string{
	"sku", "name", "description", "category", "brand", "condition", "price", "currency", "stock",
	"weight_kg", "length_cm", "width_cm", "height_cm", "days_to_ship", "image_urls",
	"tier1_name", "tier1_option", "tier2_name", "tier2_option", "variation_sku",
}

var knownColumn = func() map[string]bool {
	m := map[string]bool{}
	for _, c := range Columns {
		m[c] = true
	}
	return m
}()

// SampleCSV is a downloadable example file.
const SampleCSV = "sku,name,description,category,brand,condition,price,currency,stock,weight_kg,length_cm,width_cm,height_cm,days_to_ship,image_urls,tier1_name,tier1_option,tier2_name,tier2_option,variation_sku\n" +
	"MUG-001,Ceramic mug 350ml,Dishwasher safe ceramic mug.,100636,No Brand,NEW,89000,VND,120,0.4,10,10,12,,https://example.com/mug-front.jpg|https://example.com/mug-side.jpg,,,,,\n" +
	"TEE-001,Cotton T-shirt,100% cotton.,100017,,NEW,159000,VND,30,0.2,30,25,2,,https://example.com/tee.jpg,Color,Red,Size,M,TEE-001-RED-M\n" +
	"TEE-001,Cotton T-shirt,100% cotton.,100017,,NEW,159000,VND,25,0.2,30,25,2,,,Color,Red,Size,L,TEE-001-RED-L\n" +
	"TEE-001,Cotton T-shirt,100% cotton.,100017,,NEW,165000,VND,10,0.2,30,25,2,,,Color,Blue,Size,L,TEE-001-BLUE-L\n"

// RawRow is one data row: column -> cell text.
type RawRow map[string]string

// ParseFile reads a CSV or XLSX file into rows (header row excluded).
func ParseFile(kind string, data []byte) ([]RawRow, error) {
	if len(data) > MaxImportBytes {
		return nil, errors.New("the file is larger than 50 MB")
	}
	var next func() ([]string, error)
	switch kind {
	case "CSV":
		data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}) // UTF-8 BOM
		if !utf8.Valid(data) {
			return nil, errors.New("the CSV file must be UTF-8 encoded")
		}
		r := csv.NewReader(bytes.NewReader(data))
		r.FieldsPerRecord = -1
		r.ReuseRecord = false
		next = r.Read
	case "XLSX":
		f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{UnzipSizeLimit: xlsxUnzipLimit, UnzipXMLSizeLimit: xlsxXMLLimit})
		if err != nil {
			return nil, errors.New("the Excel file cannot be read")
		}
		defer func() { _ = f.Close() }()
		sheets := f.GetSheetList()
		if len(sheets) == 0 {
			return nil, errors.New("the Excel file has no sheet")
		}
		rows, err := f.Rows(sheets[0]) // streaming iterator
		if err != nil {
			return nil, errors.New("the first sheet cannot be read")
		}
		defer func() { _ = rows.Close() }()
		next = func() ([]string, error) {
			if !rows.Next() {
				if err := rows.Error(); err != nil {
					return nil, err
				}
				return nil, io.EOF
			}
			return rows.Columns()
		}
	default:
		return nil, errors.New("only CSV and XLSX files are supported")
	}

	header, err := next()
	if err != nil {
		return nil, errors.New("the file has no header row")
	}
	if len(header) > maxImportCols {
		return nil, fmt.Errorf("the file has more than %d columns", maxImportCols)
	}
	cols := make([]string, len(header))
	hasSKU, hasName := false, false
	for i, h := range header {
		c := strings.ToLower(strings.TrimSpace(h))
		c = strings.ReplaceAll(c, " ", "_")
		if knownColumn[c] {
			cols[i] = c
		}
		hasSKU = hasSKU || c == "sku"
		hasName = hasName || c == "name"
	}
	if !hasSKU && !hasName {
		return nil, errors.New("the header row must contain at least the columns sku and name (see the sample file)")
	}
	var out []RawRow
	for {
		rec, err := next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("row %d cannot be read", len(out)+2)
		}
		row := RawRow{}
		empty := true
		for i, v := range rec {
			if i >= len(cols) || cols[i] == "" {
				continue
			}
			if utf8.RuneCountInString(v) > maxCellLen {
				v = string([]rune(v)[:maxCellLen])
			}
			v = strings.TrimSpace(v)
			if v != "" {
				empty = false
				row[cols[i]] = v
			}
		}
		if empty {
			continue
		}
		if len(out) >= MaxImportRows {
			return nil, fmt.Errorf("the file has more than %d rows", MaxImportRows)
		}
		out = append(out, row)
	}
	if len(out) == 0 {
		return nil, errors.New("the file has no data rows")
	}
	return out, nil
}

// csvSafe neutralizes spreadsheet formulas in exported cells (R7).
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}
