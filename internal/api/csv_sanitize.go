package api

// sanitizeCSVCell prevents spreadsheet formula injection (CSV injection) by
// prefixing cell values that start with formula-triggering characters with a
// single quote. Spreadsheet applications (Excel, LibreOffice, Google Sheets)
// interpret cells starting with =, +, -, @, \t, or \r as formulas.
//
// While most fields in ROS CSV exports originate from Kubernetes names (which
// are RFC 1123 constrained and cannot start with these characters), some fields
// like cluster_alias are user-defined free text. This function is applied as
// defense-in-depth to all text fields.
//
// Reference: https://owasp.org/www-community/attacks/CSV_Injection
func sanitizeCSVCell(value string) string {
	if len(value) == 0 {
		return value
	}
	switch value[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + value
	}
	return value
}

// sanitizeCSVRow applies sanitizeCSVCell to every element in a CSV row.
func sanitizeCSVRow(row []string) []string {
	for i, cell := range row {
		row[i] = sanitizeCSVCell(cell)
	}
	return row
}
