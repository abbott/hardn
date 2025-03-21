package cmd

import (
	// "encoding/json"
	"fmt"
	// "strings"

	"github.com/abbott/hardn/pkg/adapter/secondary"
	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/domain/service"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/system"
	"github.com/spf13/cobra"
)

var (
	exportFile    string
	outputFormat  string
	sectionFilter string
	formatAsJson  bool
	formatAsYaml  bool
)

// SystemDetailsCmd returns the system command
func SystemDetailsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "system-details",
		Short: "Display detailed system summary",
		Long:  `Generate and display a comprehensive system summary including CPU, memory, disk and network information with a focus on ZFS-based systems.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSystemDetails()
		},
	}
	// Add flags
	cmd.Flags().StringVarP(&exportFile, "export", "E", "", "Export host information to file")
	cmd.Flags().StringVarP(&sectionFilter, "section", "S", "", "Display only specific section (system, network, users, storage)")
	cmd.Flags().StringVarP(&outputFormat, "format", "O", "text", "Output format (text, json, yaml)")
	cmd.Flags().BoolVar(&formatAsJson, "json", false, "Output in JSON format (shorthand)")
	cmd.Flags().BoolVar(&formatAsYaml, "yaml", false, "Output in YAML format (shorthand)")

	return cmd
}

// runSystemDetails executes the system command
func runSystemDetails() error {
	// Enable silent mode to prevent logs from appearing in output
	logging.SetSilentMode(true)
	// Create a new HostInfoService and HostInfoManager instance
	// Set up provider
	provider := interfaces.NewProvider()

	// Enable silent mode to prevent logs from appearing in output
	logging.SetSilentMode(true)
	defer logging.SetSilentMode(false) // Restore normal logging when function exits

	// Detect OS
	osInfo, err := osdetect.DetectOS()
	if err != nil {
		return fmt.Errorf("failed to detect OS: %w", err)
	}

	// Set output format from shorthand flags if they were used
	if formatAsJson {
		outputFormat = "json"
	} else if formatAsYaml {
		outputFormat = "yaml"
	}

	// Create user repository first
	userRepo := secondary.NewOSUserRepository(provider.FS, provider.Commander, osInfo.OsType)

	// Create host info repository
	hostInfoRepo := secondary.NewOSHostInfoRepository(provider.FS, provider.Commander, osInfo.OsType, userRepo)

	// Create domain service
	hostInfoService := service.NewHostInfoServiceImpl(hostInfoRepo, userRepo, model.OSInfo{
		Type:      osInfo.OsType,
		Version:   osInfo.OsVersion,
		Codename:  osInfo.OsCodename,
		IsProxmox: osInfo.IsProxmox,
	})

	// Create application service
	hostInfoManager := application.NewHostInfoManager(hostInfoService)

	// Generate system status information with our enhanced implementation
	info, err := system.GenerateSystemStatus(hostInfoManager)
	if err != nil {
		return fmt.Errorf("failed to generate system status: %w", err)
	}

	// // If export file is specified, write to file
	// if exportFile != "" {
	// 	err := exportHostInfo(exportFile, info, hostInfoManager, outputFormat)
	// 	if err != nil {
	// 		return fmt.Errorf("failed to export host information: %w", err)
	// 	}
	// 	fmt.Printf("Host information exported to %s\n", exportFile)
	// 	return nil
	// }

	// // Otherwise, print to console
	// if outputFormat == "json" {
	// 	// Output as JSON
	// 	jsonData, err := json.MarshalIndent(info, "", "  ")
	// 	if err != nil {
	// 		return fmt.Errorf("failed to marshal host information to JSON: %w", err)
	// 	}
	// 	fmt.Println(string(jsonData))
	// 	return nil
	// } else if outputFormat == "yaml" {
	// 	// For YAML we'd need to import a YAML library
	// 	// For now just indicate it's not implemented
	// 	return fmt.Errorf("YAML output format not implemented yet")
	// } else {
	// 	// Default to text output
	// 	printHostInfo(info, hostInfoManager, sectionFilter)
	// }

	// Display the formatted system status with our enhanced display function
	system.DisplayMachineStatus(info)

	return nil
}
