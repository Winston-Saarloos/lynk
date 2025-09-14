package snmp

import (
	"fmt"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
)

// PrinterStatus represents the status of a printer
type PrinterStatus struct {
	Host           string    `json:"host"`
	Model          string    `json:"model"`
	Status         string    `json:"status"`
	TonerLevel     int       `json:"toner_level"`
	PaperStatus    string    `json:"paper_status"`
	ErrorCount     int       `json:"error_count"`
	TotalPages     int       `json:"total_pages"`
	LastError      string    `json:"last_error"`
	LastSeen       time.Time `json:"last_seen"`
	Capabilities   string    `json:"capabilities"`
}

// Client represents an SNMP client for printer monitoring
type Client struct {
	community string
	timeout   time.Duration
}

// NewClient creates a new SNMP client
func NewClient(community string) *Client {
	return &Client{
		community: community,
		timeout:   10 * time.Second,
	}
}

// Poll queries a printer for its status
func (c *Client) Poll(host string) (*PrinterStatus, error) {
	// Create SNMP connection
	gosnmp.Default.Target = host
	gosnmp.Default.Community = c.community
	gosnmp.Default.Timeout = c.timeout
	gosnmp.Default.Retries = 3
	gosnmp.Default.Version = gosnmp.Version2c

	err := gosnmp.Default.Connect()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", host, err)
	}
	defer gosnmp.Default.Conn.Close()

	status := &PrinterStatus{
		Host:     host,
		LastSeen: time.Now(),
		Status:   "unknown",
	}

	// Get Brother printer information
	c.getBrotherInfo(status)
	
	// Try to get standard printer status
	c.getStandardPrinterStatus(status)
	
	// Try to get toner levels
	c.getTonerLevels(status)
	
	// Try to get page counts
	c.getPageCounts(status)
	
	// Try to get error information
	c.getErrorInfo(status)

	return status, nil
}

// getBrotherInfo gets Brother-specific printer information
func (c *Client) getBrotherInfo(status *PrinterStatus) {
	// Get the Brother printer model and capabilities
	result, err := gosnmp.Default.Get([]string{"1.3.6.1.4.1.2435.2.3.9.1.1.7.0"})
	if err == nil && len(result.Variables) > 0 {
		if result.Variables[0].Type == gosnmp.OctetString {
			value := string(result.Variables[0].Value.([]byte))
			status.Capabilities = value
			
			// Extract model from the capabilities string
			if strings.Contains(value, "MDL:") {
				parts := strings.Split(value, "MDL:")
				if len(parts) > 1 {
					modelPart := strings.Split(parts[1], ";")[0]
					status.Model = strings.TrimSpace(modelPart)
				}
			}
		}
	}
}

// getStandardPrinterStatus tries to get printer status using standard OIDs
func (c *Client) getStandardPrinterStatus(status *PrinterStatus) {
	statusOIDs := []string{
		"1.3.6.1.2.1.25.3.5.1.1.1",            // hrPrinterStatus
		"1.3.6.1.2.1.25.3.5.1.2.1",            // hrPrinterDetectedErrorState
	}

	for _, oid := range statusOIDs {
		result, err := gosnmp.Default.Get([]string{oid})
		if err == nil && len(result.Variables) > 0 {
			if result.Variables[0].Type == gosnmp.Integer {
				value := int(result.Variables[0].Value.(int))
				if oid == "1.3.6.1.2.1.25.3.5.1.1.1" {
					status.Status = c.parsePrinterStatus(value)
				} else {
					status.ErrorCount = value
					status.PaperStatus = c.parseErrorState(value)
				}
			}
		}
	}
}

// getTonerLevels tries to get toner levels using standard OIDs
func (c *Client) getTonerLevels(status *PrinterStatus) {
	tonerOIDs := []string{
		"1.3.6.1.2.1.43.11.1.1.9.1.1",         // Standard toner level
		"1.3.6.1.2.1.43.11.1.1.9.1.2",         // Alternative toner level
		"1.3.6.1.2.1.43.11.1.1.9.1.3",         // Another alternative
	}

	for _, oid := range tonerOIDs {
		result, err := gosnmp.Default.Get([]string{oid})
		if err == nil && len(result.Variables) > 0 {
			if result.Variables[0].Type == gosnmp.Integer {
				level := int(result.Variables[0].Value.(int))
				if level > 0 && level <= 100 {
					status.TonerLevel = level
					break
				}
			}
		}
	}
}

// getPageCounts tries to get page count information
func (c *Client) getPageCounts(status *PrinterStatus) {
	// Standard page count OIDs
	pageOIDs := []string{
		"1.3.6.1.2.1.43.10.2.1.4.1.1",         // Standard total pages printed
		"1.3.6.1.2.1.43.10.2.1.4.1.2",         // Alternative page count
		"1.3.6.1.4.1.2435.2.3.9.4.2.1.1.1.6.1.4", // Brother specific page count
		"1.3.6.1.4.1.2435.2.3.9.4.2.1.1.1.6.1.5", // Brother specific page count alt
	}

	for _, oid := range pageOIDs {
		result, err := gosnmp.Default.Get([]string{oid})
		if err == nil && len(result.Variables) > 0 {
			if result.Variables[0].Type == gosnmp.Integer {
				pages := int(result.Variables[0].Value.(int))
				if pages > 0 {
					status.TotalPages = pages
					break
				}
			} else if result.Variables[0].Type == gosnmp.Counter32 {
				pages := int(result.Variables[0].Value.(uint32))
				if pages > 0 {
					status.TotalPages = pages
					break
				}
			}
		}
	}
}

// getErrorInfo tries to get error information
func (c *Client) getErrorInfo(status *PrinterStatus) {
	// Try to get error descriptions
	errorOIDs := []string{
		"1.3.6.1.2.1.25.3.5.1.2.1",            // hrPrinterDetectedErrorState
		"1.3.6.1.4.1.2435.2.3.9.1.1.2.0",      // Brother error status
		"1.3.6.1.4.1.2435.2.3.9.1.1.3.0",      // Brother error description
	}

	for _, oid := range errorOIDs {
		result, err := gosnmp.Default.Get([]string{oid})
		if err == nil && len(result.Variables) > 0 {
			if result.Variables[0].Type == gosnmp.Integer {
				errorState := int(result.Variables[0].Value.(int))
				if errorState != 0 {
					status.LastError = c.parseErrorDescription(errorState)
				}
			} else if result.Variables[0].Type == gosnmp.OctetString {
				errorDesc := string(result.Variables[0].Value.([]byte))
				if errorDesc != "" {
					status.LastError = errorDesc
				}
			}
		}
	}
}

// parsePrinterStatus converts SNMP status to human readable
func (c *Client) parsePrinterStatus(status int) string {
	switch status {
	case 1:
		return "other"
	case 2:
		return "unknown"
	case 3:
		return "idle"
	case 4:
		return "printing"
	case 5:
		return "warmup"
	default:
		return "unknown"
	}
}

// parseErrorState converts error state to paper status
func (c *Client) parseErrorState(errorState int) string {
	if errorState == 0 {
		return "ok"
	}
	// Standard printer error states
	if errorState&0x01 != 0 {
		return "paper_out"
	}
	if errorState&0x02 != 0 {
		return "paper_jam"
	}
	if errorState&0x04 != 0 {
		return "toner_low"
	}
	return "error"
}

// parseErrorDescription converts error codes to human readable descriptions
func (c *Client) parseErrorDescription(errorState int) string {
	if errorState == 0 {
		return "No errors"
	}
	
	var errors []string
	if errorState&0x01 != 0 {
		errors = append(errors, "Paper out")
	}
	if errorState&0x02 != 0 {
		errors = append(errors, "Paper jam")
	}
	if errorState&0x04 != 0 {
		errors = append(errors, "Toner low")
	}
	if errorState&0x08 != 0 {
		errors = append(errors, "Door open")
	}
	if errorState&0x10 != 0 {
		errors = append(errors, "Toner empty")
	}
	if errorState&0x20 != 0 {
		errors = append(errors, "Service required")
	}
	
	if len(errors) == 0 {
		return fmt.Sprintf("Unknown error (code: %d)", errorState)
	}
	
	return strings.Join(errors, ", ")
}

// String returns a nicely formatted string representation of the printer status
func (p *PrinterStatus) String() string {
	var output strings.Builder
	
	output.WriteString(fmt.Sprintf("🖨️  Printer: %s\n", p.Host))
	output.WriteString(fmt.Sprintf("   Model: %s\n", p.Model))
	output.WriteString(fmt.Sprintf("   Status: %s\n", p.Status))
	
	if p.TonerLevel > 0 {
		output.WriteString(fmt.Sprintf("   Toner Level: %d%%\n", p.TonerLevel))
	} else {
		output.WriteString("   Toner Level: Unknown\n")
	}
	
	output.WriteString(fmt.Sprintf("   Paper Status: %s\n", p.PaperStatus))
	output.WriteString(fmt.Sprintf("   Total Pages Printed: %d\n", p.TotalPages))
	output.WriteString(fmt.Sprintf("   Error Count: %d\n", p.ErrorCount))
	
	if p.LastError != "" {
		output.WriteString(fmt.Sprintf("   Last Error: %s\n", p.LastError))
	}
	
	output.WriteString(fmt.Sprintf("   Last Checked: %s\n", p.LastSeen.Format("2006-01-02 15:04:05")))
	
	// Show capabilities in a cleaner format
	if p.Capabilities != "" {
		output.WriteString("   Capabilities:\n")
		capabilities := strings.Split(p.Capabilities, ";")
		for _, cap := range capabilities {
			if cap != "" {
				parts := strings.Split(cap, ":")
				if len(parts) == 2 {
					output.WriteString(fmt.Sprintf("     %s: %s\n", parts[0], parts[1]))
				}
			}
		}
	}
	
	return output.String()
}
