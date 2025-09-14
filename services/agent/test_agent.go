package main

import (
	"fmt"
	"log"

	"lynk/agent/internal/scheduler"
	"lynk/agent/internal/snmp"
)

func main() {
	// Test with your Brother printer
	printers := []string{"192.168.50.250"} // Your Brother printer IP

	// Create SNMP client
	client := snmp.NewClient("public")

	// Create scheduler with 2 worker goroutines
	s := scheduler.New(2)

	fmt.Println("Starting printer monitoring agent...")
	fmt.Println("This will attempt to poll printers and show results/errors")

	for _, host := range printers {
		h := host
		s.Submit(func() {
			fmt.Printf("Polling printer at %s...\n", h)
			result, err := client.Poll(h)
			if err != nil {
				log.Printf("Error polling %s: %v", h, err)
				return
			}
			log.Printf("Printer %s → %+v", h, result)
		})
	}

	s.Wait()
	fmt.Println("Monitoring complete!")
}