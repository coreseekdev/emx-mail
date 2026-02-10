package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/emx-mail/cli/pkgs/config"
)

func handleInit() error {
	root := config.ExampleRootConfig()

	if config.HasEmxConfig() {
		data, err := json.MarshalIndent(root, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to format example config: %w", err)
		}

		fmt.Println("emx-config detected. Configure emx-mail using emx-config.")
		fmt.Println("Example JSON (keys under 'mail'):")
		fmt.Println(string(data))
		fmt.Println("Store this in your emx-config file (e.g., config.json).")
		fmt.Println("Then verify with: emx-config list --json")
		return nil
	}

	configPath, err := config.GetEnvConfigPath()
	if err != nil {
		return err
	}
	if err := config.SaveConfig(configPath, root); err != nil {
		return err
	}
	fmt.Printf("Created config file at: %s\n", configPath)
	if os.Getenv(config.EnvConfigJSONPath) == "" {
		fmt.Printf("Tip: set %s=%s to use this config file.\n", config.EnvConfigJSONPath, configPath)
	}
	fmt.Println("Please edit the file to add your email account credentials.")
	return nil
}
