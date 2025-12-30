package db

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ProfileInfo struct {
	Name string
	Path string
}

func FindAllProfiles() ([]ProfileInfo, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	var firefoxDir string
	var browserName string

	// Linux paths
	switch {
	case fileExists(filepath.Join(homeDir, ".librewolf")):
		firefoxDir = filepath.Join(homeDir, ".librewolf")
		browserName = "LibreWolf"
	case fileExists(filepath.Join(homeDir, ".mozilla", "firefox")):
		firefoxDir = filepath.Join(homeDir, ".mozilla", "firefox")
		browserName = "Firefox"
	// macOS paths
	case fileExists(filepath.Join(homeDir, "Library", "Application Support", "LibreWolf")):
		firefoxDir = filepath.Join(homeDir, "Library", "Application Support", "LibreWolf")
		browserName = "LibreWolf"
	case fileExists(filepath.Join(homeDir, "Library", "Application Support", "Firefox")):
		firefoxDir = filepath.Join(homeDir, "Library", "Application Support", "Firefox")
		browserName = "Firefox"
	// Windows paths
	case fileExists(filepath.Join(homeDir, "AppData", "Roaming", "LibreWolf")):
		firefoxDir = filepath.Join(homeDir, "AppData", "Roaming", "LibreWolf")
		browserName = "LibreWolf"
	case fileExists(filepath.Join(homeDir, "AppData", "Roaming", "Mozilla", "Firefox")):
		firefoxDir = filepath.Join(homeDir, "AppData", "Roaming", "Mozilla", "Firefox")
		browserName = "Firefox"
	default:
		return nil, fmt.Errorf("firefox/librewolf profile directory not found")
	}

	fmt.Printf("Found %s profile directory: %s\n\n", browserName, firefoxDir)

	profilesIni := filepath.Join(firefoxDir, "profiles.ini")
	if !fileExists(profilesIni) {
		return nil, fmt.Errorf("profiles.ini not found at %s", profilesIni)
	}

	profiles, err := parseAllProfiles(profilesIni, firefoxDir)
	if err != nil {
		return nil, fmt.Errorf("failed to parse profiles.ini: %w", err)
	}

	var validProfiles []ProfileInfo
	for _, profile := range profiles {
		placesDB := filepath.Join(firefoxDir, profile.Path, "places.sqlite")
		if fileExists(placesDB) {
			validProfiles = append(validProfiles, ProfileInfo{
				Name: profile.Name,
				Path: placesDB,
			})
		}
	}

	if len(validProfiles) == 0 {
		return nil, fmt.Errorf("no profiles with places.sqlite found")
	}

	return validProfiles, nil
}

func parseAllProfiles(path string, baseDir string) ([]ProfileInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var profiles []ProfileInfo
	var currentName string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "[Profile") && strings.HasSuffix(line, "]") {
			currentName = ""
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		if key == "Name" {
			currentName = value
		}

		if key == "Path" && currentName != "" {
			profiles = append(profiles, ProfileInfo{
				Name: currentName,
				Path: value,
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return profiles, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
