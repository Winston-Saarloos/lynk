package snmp

import (
	"fmt"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
)

// PrinterStatus represents the status of a printer (MVP Data Set)
type PrinterStatus struct {
	// Device Identity
	Host           string    `json:"host"`
	Model          string    `json:"model"`
	SerialNumber   string    `json:"serial_number"`
	FirmwareVersion string   `json:"firmware_version"`
	DeviceName     string    `json:"device_name"`        // sysName.0
	PrinterName    string    `json:"printer_name"`       // prtGeneralPrinterName.1
	SystemDescription string `json:"system_description"` // sysDescr.0
	
	// Device Status
	Status         string    `json:"status"`             // prtGeneralPrinterStatus.1
	Uptime         uint32    `json:"uptime"`             // sysUpTime.0 (TimeTicks)
	DeviceStatus   int       `json:"device_status"`      // hrDeviceStatus
	
	// Page Counters
	TotalPages     int       `json:"total_pages"`        // prtMarkerLifeCount
	PageCounterUnit int      `json:"page_counter_unit"`  // prtMarkerCounterUnit
	
	// Consumables
	TonerLevel     int       `json:"toner_level"`        // prtMarkerSuppliesLevel (toner)
	TonerMaxCapacity int     `json:"toner_max_capacity"` // prtMarkerSuppliesMaxCapacity (toner)
	DrumLevel      int       `json:"drum_level"`         // prtMarkerSuppliesLevel (drum)
	DrumMaxCapacity int      `json:"drum_max_capacity"`  // prtMarkerSuppliesMaxCapacity (drum)
	
	// Alerts/Errors
	ErrorCount     int       `json:"error_count"`        // prtAlertTable count
	LastError      string    `json:"last_error"`         // prtAlertTable latest
	ActiveAlerts   []string  `json:"active_alerts"`      // prtAlertTable details
	
	// Paper Input/Trays
	PaperTrays     []PaperTray `json:"paper_trays"`      // prtInputTable
	
	// Legacy fields (for backward compatibility)
	MemorySize     string    `json:"memory_size"`
	PaperStatus    string    `json:"paper_status"`
	DrumCount      int       `json:"drum_count"`
	DrumLifeRemaining int    `json:"drum_life_remaining"`
	AverageCoverage float64  `json:"average_coverage"`
	TotalPaperJams int       `json:"total_paper_jams"`
	TonerReplaceCount int    `json:"toner_replace_count"`
	DrumReplaceCount int     `json:"drum_replace_count"`
	LastSeen       time.Time `json:"last_seen"`
	Capabilities   string    `json:"capabilities"`
}

// PaperTray represents a paper input tray
type PaperTray struct {
	Index    int    `json:"index"`
	Name     string `json:"name"`     // prtInputName
	Status   int    `json:"status"`   // prtInputStatus (1=other, 2=unknown, 3=empty, 4=full, 5=ok)
	Capacity int    `json:"capacity"` // prtInputCapacity
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
	
	// Try to get additional Brother-specific information
	c.getBrotherMaintenanceInfo(status)
	
	// Get MVP Data Set - Device Identity
	c.getDeviceIdentity(status)
	
	// Get MVP Data Set - Device Status
	c.getDeviceStatus(status)
	
	// Get MVP Data Set - Page Counters
	c.getPageCounters(status)
	
	// Get MVP Data Set - Alerts/Errors
	c.getAlertsAndErrors(status)
	
	// Get MVP Data Set - Paper Input/Trays
	c.getPaperTrays(status)

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

// getTonerLevels tries to get toner level information using standard Printer-MIB
func (c *Client) getTonerLevels(status *PrinterStatus) {
	// Walk the prtMarkerSuppliesTable to find toner information
	baseOID := "1.3.6.1.2.1.43.11.1.1"
	
	// Store supplies data by index
	suppliesData := make(map[string]map[string]interface{})
	
	// Walk the supplies table
	err := gosnmp.Default.Walk(baseOID, func(variable gosnmp.SnmpPDU) error {
		oid := variable.Name
		parts := strings.Split(oid, ".")
		if len(parts) >= 4 {
			// For OIDs like .1.3.6.1.2.1.43.11.1.1.5.1.1
			// The structure is: baseOID.subOID.index1.index2
			// subOID is the part that identifies what type of data (5=class, 6=description, etc.)
			// index1 and index2 together form the supply index
			subOID := parts[len(parts)-3]
			index1 := parts[len(parts)-2]
			index2 := parts[len(parts)-1]
			index := index1 + "." + index2 // Combine both index parts
			
			
			// Initialize index if not exists
			if suppliesData[index] == nil {
				suppliesData[index] = make(map[string]interface{})
			}
			
			// Store the value based on sub-OID
			switch subOID {
			case "5": // prtMarkerSuppliesClass
				if variable.Type == gosnmp.Integer {
					class := int(variable.Value.(int))
					suppliesData[index]["class"] = class
				}
			case "6": // prtMarkerSuppliesDescription
				if variable.Type == gosnmp.OctetString {
					desc := string(variable.Value.([]byte))
					suppliesData[index]["description"] = desc
				}
			case "8": // prtMarkerSuppliesMaxCapacity
				if variable.Type == gosnmp.Integer {
					maxCap := int(variable.Value.(int))
					suppliesData[index]["maxCapacity"] = maxCap
				}
			case "9": // prtMarkerSuppliesLevel
				if variable.Type == gosnmp.Integer {
					currLevel := int(variable.Value.(int))
					suppliesData[index]["currentLevel"] = currLevel
				}
			}
		}
		return nil
	})
	
	if err != nil {
		return // Skip toner detection if walk fails
	}
	
	// Find toner supplies and calculate percentage
	for _, data := range suppliesData {
		class, hasClass := data["class"]
		if hasClass && class.(int) == 3 { // Class 3 = Toner
			description, _ := data["description"].(string)
			maxCapacity, hasMax := data["maxCapacity"].(int)
			currentLevel, hasCurrent := data["currentLevel"].(int)
			
			// Check if we have valid capacity data
			if hasMax && hasCurrent && maxCapacity > 0 && currentLevel >= 0 {
				// Calculate percentage
				percentage := (currentLevel * 100) / maxCapacity
				status.TonerLevel = percentage
				break
			} else if hasCurrent {
				// Handle special values
				switch currentLevel {
				case -2:
					status.TonerLevel = -1 // Unknown
				case -3:
					status.TonerLevel = -1 // Not applicable
				}
			}
			
			
			// Log what we found for debugging
			if description != "" {
				// We found toner but couldn't calculate percentage
				// This is common with Brother printers
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
				// Handle different possible types for Counter32 safely
				switch v := result.Variables[0].Value.(type) {
				case uint32:
					pages := int(v)
					if pages > 0 {
						status.TotalPages = pages
						break
					}
				case uint:
					pages := int(v)
					if pages > 0 {
						status.TotalPages = pages
						break
					}
				case int:
					pages := v
					if pages > 0 {
						status.TotalPages = pages
						break
					}
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

	// getBrotherMaintenanceInfo tries to get Brother-specific maintenance information
	func (c *Client) getBrotherMaintenanceInfo(status *PrinterStatus) {
		// Brother-specific OIDs for maintenance information (from verified mapping table)
		maintenanceOIDs := []string{
			"1.3.6.1.4.1.2435.2.4.3.99.3.1.6.1.2.1",  // Model Name: MODEL="HL-L2360D series"
			"1.3.6.1.4.1.2435.2.4.3.99.3.1.6.1.2.2",  // Serial Number: SERIAL="U63883E4N132987"
			"1.3.6.1.2.1.1.5.0",                       // Device Name: BRN30055C465129
			"1.3.6.1.4.1.2435.2.4.3.99.3.1.6.1.2.7",  // Main Firmware: FIRMVER="1.38"
			"1.3.6.1.4.1.2435.2.4.3.99.3.1.6.1.2.8",  // Sub1 Firmware ID: FIRMID="SUB1"
			"1.3.6.1.4.1.2435.2.4.3.99.3.1.6.1.2.9",  // Sub1 Firmware: FIRMVER="1.03"
			"1.3.6.1.2.1.1.3.0",                       // Uptime (TimeTicks)
			"1.3.6.1.2.1.25.3.5.1.1.1",               // Device Status (1=unknown, 2=running, 3=warning, 4=testing, 5=down)
			"1.3.6.1.2.1.43.10.2.1.4.1.1",            // Page Counter: 1536 (matches web interface!)
			"1.3.6.1.4.1.2435.2.3.9.2.1.2.9.0",       // Brother paper jams (discovered: value 2)
		}

	for _, oid := range maintenanceOIDs {
		result, err := gosnmp.Default.Get([]string{oid})
		if err == nil && len(result.Variables) > 0 {
			variable := result.Variables[0]
			
			// Try to extract information based on OID
			switch oid {
			case "1.3.6.1.4.1.2435.2.4.3.99.3.1.6.1.2.1": // Model Name
				if variable.Type == gosnmp.OctetString {
					value := string(variable.Value.([]byte))
					// Extract model from MODEL="HL-L2360D series"
					if strings.Contains(value, "MODEL=") {
						parts := strings.Split(value, "=")
						if len(parts) > 1 {
							status.Model = strings.Trim(parts[1], "\"")
						}
					}
				}
			case "1.3.6.1.4.1.2435.2.4.3.99.3.1.6.1.2.2": // Serial Number
				if variable.Type == gosnmp.OctetString {
					value := string(variable.Value.([]byte))
					// Extract serial from SERIAL="U63883E4N132987"
					if strings.Contains(value, "SERIAL=") {
						parts := strings.Split(value, "=")
						if len(parts) > 1 {
							status.SerialNumber = strings.Trim(parts[1], "\"")
						}
					}
				}
			case "1.3.6.1.2.1.1.5.0": // Device Name
				if variable.Type == gosnmp.OctetString {
					value := string(variable.Value.([]byte))
					if value != "" && status.SerialNumber == "" {
						// Use as fallback serial number if we don't have the proper one
						status.SerialNumber = value
					}
				}
			case "1.3.6.1.4.1.2435.2.4.3.99.3.1.6.1.2.7": // Main Firmware
				if variable.Type == gosnmp.OctetString {
					value := string(variable.Value.([]byte))
					// Extract firmware from FIRMVER="1.38"
					if strings.Contains(value, "FIRMVER=") {
						parts := strings.Split(value, "=")
						if len(parts) > 1 {
							status.FirmwareVersion = strings.Trim(parts[1], "\"")
						}
					}
				}
			case "1.3.6.1.4.1.2435.2.4.3.99.3.1.6.1.2.9": // Sub1 Firmware
				if variable.Type == gosnmp.OctetString {
					value := string(variable.Value.([]byte))
					// Extract sub firmware from FIRMVER="1.03"
					if strings.Contains(value, "FIRMVER=") {
						parts := strings.Split(value, "=")
						if len(parts) > 1 && status.FirmwareVersion != "" {
							// Append sub firmware to main firmware
							status.FirmwareVersion += " / Sub1: " + strings.Trim(parts[1], "\"")
						}
					}
				}
			case "1.3.6.1.2.1.25.3.5.1.1.1": // Device Status
				if variable.Type == gosnmp.Integer {
					statusCode := int(variable.Value.(int))
					switch statusCode {
					case 1:
						status.Status = "Unknown"
					case 2:
						status.Status = "Running"
					case 3:
						status.Status = "Warning"
					case 4:
						status.Status = "Testing"
					case 5:
						status.Status = "Down"
					default:
						status.Status = fmt.Sprintf("Status %d", statusCode)
					}
				}
			case "1.3.6.1.2.1.43.10.2.1.4.1.1": // Page Counter (matches web interface!)
				if variable.Type == gosnmp.Counter32 {
					switch v := variable.Value.(type) {
					case uint32:
						status.TotalPages = int(v)
					case uint:
						status.TotalPages = int(v)
					case int:
						status.TotalPages = v
					}
				}
			case "1.3.6.1.4.1.2435.2.3.9.2.1.2.9.0": // Brother paper jams
				if variable.Type == gosnmp.Integer {
					status.TotalPaperJams = int(variable.Value.(int))
				} else if variable.Type == gosnmp.Counter32 {
					switch v := variable.Value.(type) {
					case uint32:
						status.TotalPaperJams = int(v)
					case uint:
						status.TotalPaperJams = int(v)
					case int:
						status.TotalPaperJams = v
					}
				}
			}
		}
	}
}

// String returns a nicely formatted string representation of the printer status
func (p *PrinterStatus) String() string {
	var output strings.Builder
	
	output.WriteString(fmt.Sprintf("Printer: %s\n", p.Host))
	
	// Device Identity
	output.WriteString("   === DEVICE IDENTITY ===\n")
	output.WriteString(fmt.Sprintf("   Model: %s\n", p.Model))
	
	if p.SerialNumber != "" {
		output.WriteString(fmt.Sprintf("   Serial Number: %s\n", p.SerialNumber))
	}
	
	if p.FirmwareVersion != "" {
		output.WriteString(fmt.Sprintf("   Firmware Version: %s\n", p.FirmwareVersion))
	}
	
	if p.DeviceName != "" {
		output.WriteString(fmt.Sprintf("   Device Name: %s\n", p.DeviceName))
	}
	
	if p.PrinterName != "" {
		output.WriteString(fmt.Sprintf("   Printer Name: %s\n", p.PrinterName))
	}
	
	if p.SystemDescription != "" {
		output.WriteString(fmt.Sprintf("   System Description: %s\n", p.SystemDescription))
	}
	
	// Device Status
	output.WriteString("   === DEVICE STATUS ===\n")
	output.WriteString(fmt.Sprintf("   Status: %s\n", p.Status))
	
	if p.Uptime > 0 {
		// Convert TimeTicks to hours (TimeTicks are in hundredths of a second)
		hours := p.Uptime / 360000
		output.WriteString(fmt.Sprintf("   Uptime: %d hours\n", hours))
	}
	
	if p.DeviceStatus > 0 {
		output.WriteString(fmt.Sprintf("   Device Status Code: %d\n", p.DeviceStatus))
	}
	
	if p.TonerLevel > 0 {
		output.WriteString(fmt.Sprintf("   Toner Level: %d%%\n", p.TonerLevel))
	} else if p.TonerLevel == -1 {
		output.WriteString("   Toner Level: Unknown (Brother printers often don't provide continuous toner monitoring)\n")
	} else {
		output.WriteString("   Toner Level: Unknown\n")
	}
	
	// Page Counters
	output.WriteString("   === PAGE COUNTERS ===\n")
	output.WriteString(fmt.Sprintf("   Total Pages Printed: %d\n", p.TotalPages))
	
	if p.PageCounterUnit > 0 {
		output.WriteString(fmt.Sprintf("   Counter Unit: %d\n", p.PageCounterUnit))
	}
	
	// Consumables
	output.WriteString("   === CONSUMABLES ===\n")
	
	if p.DrumLevel > 0 && p.DrumMaxCapacity > 0 {
		drumPercent := (p.DrumLevel * 100) / p.DrumMaxCapacity
		output.WriteString(fmt.Sprintf("   Drum Level: %d%% (%d/%d)\n", drumPercent, p.DrumLevel, p.DrumMaxCapacity))
	}
	
	if p.DrumCount > 0 {
		output.WriteString(fmt.Sprintf("   Drum Count: %d\n", p.DrumCount))
	}
	
	if p.DrumLifeRemaining > 0 {
		output.WriteString(fmt.Sprintf("   Drum Life Remaining: %d pages\n", p.DrumLifeRemaining))
	}
	
	if p.AverageCoverage > 0 {
		output.WriteString(fmt.Sprintf("   Average Coverage: %.2f%%\n", p.AverageCoverage))
	}
	
	if p.TotalPaperJams > 0 {
		output.WriteString(fmt.Sprintf("   Total Paper Jams: %d\n", p.TotalPaperJams))
	}
	
	if p.TonerReplaceCount > 0 {
		output.WriteString(fmt.Sprintf("   Toner Replace Count: %d\n", p.TonerReplaceCount))
	}
	
	if p.DrumReplaceCount > 0 {
		output.WriteString(fmt.Sprintf("   Drum Replace Count: %d\n", p.DrumReplaceCount))
	}
	
	// Alerts/Errors
	output.WriteString("   === ALERTS / ERRORS ===\n")
	output.WriteString(fmt.Sprintf("   Error Count: %d\n", p.ErrorCount))
	
	if p.LastError != "" {
		output.WriteString(fmt.Sprintf("   Last Error: %s\n", p.LastError))
	}
	
	if len(p.ActiveAlerts) > 0 {
		output.WriteString("   Active Alerts:\n")
		for _, alert := range p.ActiveAlerts {
			output.WriteString(fmt.Sprintf("     - %s\n", alert))
		}
	}
	
	// Paper Input/Trays
	if len(p.PaperTrays) > 0 {
		output.WriteString("   === PAPER TRAYS ===\n")
		for _, tray := range p.PaperTrays {
			var statusText string
			switch tray.Status {
			case 1:
				statusText = "Other"
			case 2:
				statusText = "Unknown"
			case 3:
				statusText = "Empty"
			case 4:
				statusText = "Full"
			case 5:
				statusText = "OK"
			default:
				statusText = fmt.Sprintf("Status %d", tray.Status)
			}
			output.WriteString(fmt.Sprintf("   %s: %s (Capacity: %d)\n", tray.Name, statusText, tray.Capacity))
		}
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

// getDeviceIdentity collects device identity information (MVP Data Set)
func (c *Client) getDeviceIdentity(status *PrinterStatus) {
	identityOIDs := []string{
		"1.3.6.1.2.1.1.1.0",                    // sysDescr.0 - general description
		"1.3.6.1.2.1.1.5.0",                    // sysName.0 - device hostname
		"1.3.6.1.2.1.43.5.1.1.16.1",           // prtGeneralPrinterName.1 - friendly printer name
	}

	for _, oid := range identityOIDs {
		result, err := gosnmp.Default.Get([]string{oid})
		if err == nil && len(result.Variables) > 0 {
			variable := result.Variables[0]
			if variable.Type == gosnmp.OctetString {
				value := string(variable.Value.([]byte))
				switch oid {
				case "1.3.6.1.2.1.1.1.0": // sysDescr.0
					status.SystemDescription = value
				case "1.3.6.1.2.1.1.5.0": // sysName.0
					status.DeviceName = value
				case "1.3.6.1.2.1.43.5.1.1.16.1": // prtGeneralPrinterName.1
					if value != "" {
						status.PrinterName = value
					}
				}
			}
		}
	}
}

// getDeviceStatus collects device status information (MVP Data Set)
func (c *Client) getDeviceStatus(status *PrinterStatus) {
	statusOIDs := []string{
		"1.3.6.1.2.1.43.5.1.1.1.1",            // prtGeneralPrinterStatus.1
		"1.3.6.1.2.1.1.3.0",                    // sysUpTime.0
		"1.3.6.1.2.1.25.3.5.1.1.1",            // hrDeviceStatus
	}

	for _, oid := range statusOIDs {
		result, err := gosnmp.Default.Get([]string{oid})
		if err == nil && len(result.Variables) > 0 {
			variable := result.Variables[0]
			switch oid {
			case "1.3.6.1.2.1.43.5.1.1.1.1": // prtGeneralPrinterStatus.1
				if variable.Type == gosnmp.Integer {
					statusCode := int(variable.Value.(int))
					switch statusCode {
					case 0:
						status.Status = "Idle"
					case 3:
						status.Status = "Idle"
					case 4:
						status.Status = "Printing"
					case 5:
						status.Status = "Warmup"
					default:
						status.Status = fmt.Sprintf("Status %d", statusCode)
					}
				}
			case "1.3.6.1.2.1.1.3.0": // sysUpTime.0
				if variable.Type == gosnmp.TimeTicks {
					status.Uptime = variable.Value.(uint32)
				}
			case "1.3.6.1.2.1.25.3.5.1.1.1": // hrDeviceStatus
				if variable.Type == gosnmp.Integer {
					status.DeviceStatus = int(variable.Value.(int))
				}
			}
		}
	}
}

// getPageCounters collects page counter information (MVP Data Set)
func (c *Client) getPageCounters(status *PrinterStatus) {
	pageOIDs := []string{
		"1.3.6.1.2.1.43.10.2.1.4.1.1",         // prtMarkerLifeCount.1.1
		"1.3.6.1.2.1.43.10.2.1.3.1.1",         // prtMarkerCounterUnit.1.1
	}

	for _, oid := range pageOIDs {
		result, err := gosnmp.Default.Get([]string{oid})
		if err == nil && len(result.Variables) > 0 {
			variable := result.Variables[0]
			switch oid {
			case "1.3.6.1.2.1.43.10.2.1.4.1.1": // prtMarkerLifeCount.1.1
				if variable.Type == gosnmp.Counter32 {
					switch v := variable.Value.(type) {
					case uint32:
						status.TotalPages = int(v)
					case uint:
						status.TotalPages = int(v)
					case int:
						status.TotalPages = v
					}
				}
			case "1.3.6.1.2.1.43.10.2.1.3.1.1": // prtMarkerCounterUnit.1.1
				if variable.Type == gosnmp.Integer {
					status.PageCounterUnit = int(variable.Value.(int))
				}
			}
		}
	}
}

// getAlertsAndErrors collects alert and error information (MVP Data Set)
func (c *Client) getAlertsAndErrors(status *PrinterStatus) {
	// Walk the prtAlertTable
	status.ActiveAlerts = []string{}
	alertCount := 0
	
	err := gosnmp.Default.Walk("1.3.6.1.2.1.43.18.1.1", func(variable gosnmp.SnmpPDU) error {
		alertCount++
		oid := variable.Name
		var valueStr string
		
		if variable.Type == gosnmp.OctetString {
			valueStr = string(variable.Value.([]byte))
		} else if variable.Type == gosnmp.Integer {
			valueStr = fmt.Sprintf("%d", variable.Value.(int))
		} else {
			valueStr = fmt.Sprintf("%v", variable.Value)
		}
		
		// Only include non-zero alerts
		if valueStr != "0" && valueStr != "" {
			status.ActiveAlerts = append(status.ActiveAlerts, fmt.Sprintf("%s: %s", oid, valueStr))
		}
		
		return nil
	})
	
	if err == nil {
		status.ErrorCount = len(status.ActiveAlerts)
		if len(status.ActiveAlerts) > 0 {
			status.LastError = status.ActiveAlerts[0] // First active alert
		}
	}
}

// getPaperTrays collects paper input/tray information (MVP Data Set)
func (c *Client) getPaperTrays(status *PrinterStatus) {
	status.PaperTrays = []PaperTray{}
	
	// Walk the prtInputTable to collect tray information
	trayData := make(map[string]map[string]interface{})
	
	err := gosnmp.Default.Walk("1.3.6.1.2.1.43.8.2.1", func(variable gosnmp.SnmpPDU) error {
		oid := variable.Name
		parts := strings.Split(oid, ".")
		if len(parts) >= 4 {
			// For OIDs like .1.3.6.1.2.1.43.8.2.1.13.1.1
			// The structure is: baseOID.subOID.index1.index2
			subOID := parts[len(parts)-3]
			index1 := parts[len(parts)-2]
			index2 := parts[len(parts)-1]
			index := index1 + "." + index2
			
			if trayData[index] == nil {
				trayData[index] = make(map[string]interface{})
			}
			
			switch subOID {
			case "13": // prtInputName
				if variable.Type == gosnmp.OctetString {
					trayData[index]["name"] = string(variable.Value.([]byte))
				}
			case "18": // prtInputStatus
				if variable.Type == gosnmp.Integer {
					trayData[index]["status"] = int(variable.Value.(int))
				}
			case "9": // prtInputCapacity
				if variable.Type == gosnmp.Integer {
					trayData[index]["capacity"] = int(variable.Value.(int))
				}
			}
		}
		return nil
	})
	
	if err == nil {
		// Convert tray data to PaperTray structs
		for _, data := range trayData {
			name, hasName := data["name"].(string)
			trayStatus, hasStatus := data["status"].(int)
			capacity, _ := data["capacity"].(int)
			
			if hasName && name != "" && hasStatus {
				tray := PaperTray{
					Index:    1, // We'll use a simple index
					Name:     name,
					Status:   trayStatus,
					Capacity: capacity,
				}
				status.PaperTrays = append(status.PaperTrays, tray)
			}
		}
	}
}
