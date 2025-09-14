package main

import (
	"fmt"
	"log"
	"strings"

	"lynk/agent/internal/scheduler"
	"lynk/agent/internal/snmp"
)

func main() {
	// Your Brother printer
	printers := []string{"192.168.50.250"}

	// Create SNMP client
	client := snmp.NewClient("public")

	// Create scheduler with 5 worker goroutines
	s := scheduler.New(5)

	fmt.Println("🔍 Starting printer monitoring...")
	fmt.Println(strings.Repeat("=", 50))

	for _, host := range printers {
		h := host
		s.Submit(func() {
			result, err := client.Poll(h)
			if err != nil {
				log.Printf("❌ Error polling %s: %v", h, err)
				return
			}
			fmt.Println(result.String())
		})
	}

	s.Wait()
	fmt.Println("✅ Monitoring complete!")
}