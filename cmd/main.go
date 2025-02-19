package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

type ExportOption int

const (
	LocalFolder ExportOption = iota
	GoogleSheets
)

type GoogleConfig struct {
	sheetsService *sheets.Service
	spreadsheetId string
	useApiKey     bool
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nReceived interrupt signal. Shutting down...")
		cancel()
	}()

	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Enter source folder path containing hex files: ")
	srcFolder, _ := reader.ReadString('\n')
	srcFolder = strings.TrimSpace(srcFolder)

	fmt.Println("\nChoose export option:")
	fmt.Println("1. Export to local folder")
	fmt.Println("2. Export to Google Sheets")

	var choice string
	fmt.Print("Enter your choice (1 or 2): ")
	choice, _ = reader.ReadString('\n')
	choice = strings.TrimSpace(choice)

	var exportOption ExportOption
	var dstFolder string
	var googleConfig *GoogleConfig

	switch choice {
	case "1":
		exportOption = LocalFolder
		fmt.Print("Enter destination folder path for ASCII files: ")
		dstFolder, _ = reader.ReadString('\n')
		dstFolder = strings.TrimSpace(dstFolder)
		if err := os.MkdirAll(dstFolder, 0755); err != nil {
			fmt.Printf("Error creating destination folder: %v\n", err)
			return
		}
	case "2":
		exportOption = GoogleSheets
		fmt.Println("\nChoose authentication method:")
		fmt.Println("1. API Key")
		fmt.Println("2. Service Account Credentials")

		var authChoice string
		fmt.Print("Enter your choice (1 or 2): ")
		authChoice, _ = reader.ReadString('\n')
		authChoice = strings.TrimSpace(authChoice)

		var err error
		switch authChoice {
		case "1":
			fmt.Print("Enter Google Sheets API Key: ")
			apiKey, _ := reader.ReadString('\n')
			apiKey = strings.TrimSpace(apiKey)
			googleConfig, err = setupGoogleSheetsWithApiKey(apiKey)
		case "2":
			fmt.Print("Enter path to Google credentials JSON file: ")
			credPath, _ := reader.ReadString('\n')
			credPath = strings.TrimSpace(credPath)
			googleConfig, err = setupGoogleSheetsWithCredentials(credPath)
		default:
			fmt.Println("Invalid authentication choice")
			return
		}

		if err != nil {
			fmt.Printf("Error setting up Google Sheets: %v\n", err)
			return
		}

		fmt.Print("Enter spreadsheet ID (or leave empty to create new): ")
		googleConfig.spreadsheetId, _ = reader.ReadString('\n')
		googleConfig.spreadsheetId = strings.TrimSpace(googleConfig.spreadsheetId)
	default:
		fmt.Println("Invalid choice")
		return
	}

	processFiles(ctx, srcFolder, dstFolder, exportOption, googleConfig)
}

func setupGoogleSheetsWithApiKey(apiKey string) (*GoogleConfig, error) {
	ctx := context.Background()
	srv, err := sheets.NewService(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("unable to create sheets service: %v", err)
	}

	return &GoogleConfig{
		sheetsService: srv,
		useApiKey:     true,
	}, nil
}

func setupGoogleSheetsWithCredentials(credentialsPath string) (*GoogleConfig, error) {
	ctx := context.Background()

	b, err := ioutil.ReadFile(credentialsPath)
	if err != nil {
		return nil, fmt.Errorf("unable to read credentials file: %v", err)
	}

	config, err := google.JWTConfigFromJSON(b, sheets.SpreadsheetsScope)
	if err != nil {
		return nil, fmt.Errorf("unable to parse credentials: %v", err)
	}

	client := config.Client(ctx)

	srv, err := sheets.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("unable to create sheets service: %v", err)
	}

	return &GoogleConfig{
		sheetsService: srv,
		useApiKey:     false,
	}, nil
}

func processFiles(ctx context.Context, srcFolder, dstFolder string, exportOption ExportOption, googleConfig *GoogleConfig) {
	files, err := ioutil.ReadDir(srcFolder)
	if err != nil {
		fmt.Printf("Error reading source folder: %v\n", err)
		return
	}

	for _, file := range files {
		select {
		case <-ctx.Done():
			fmt.Println("Processing interrupted.")
			return
		default:
		}

		if file.IsDir() {
			continue
		}

		srcPath := filepath.Join(srcFolder, file.Name())

		switch exportOption {
		case LocalFolder:
			dstPath := filepath.Join(dstFolder, strings.TrimSuffix(file.Name(), filepath.Ext(file.Name()))+".txt")
			if fileExists(dstPath) {
				fmt.Printf("Skipping %s: Already converted\n", file.Name())
				continue
			}
			if err := convertFile(srcPath, dstPath); err != nil {
				fmt.Printf("Error converting %s: %v\n", file.Name(), err)
				continue
			}
		case GoogleSheets:
			if err := exportToGoogleSheets(srcPath, file.Name(), googleConfig); err != nil {
				fmt.Printf("Error exporting %s to Google Sheets: %v\n", file.Name(), err)
				continue
			}
		}

		fmt.Printf("Processed %s successfully\n", file.Name())
	}
}

func exportToGoogleSheets(srcPath, fileName string, config *GoogleConfig) error {
	hexData, err := ioutil.ReadFile(srcPath)
	if err != nil {
		return err
	}

	asciiStr, err := HexToAscii(string(hexData))
	if err != nil {
		return err
	}

	if config.spreadsheetId == "" && config.useApiKey {
		return fmt.Errorf("cannot create new spreadsheet with API key authentication. Please provide an existing spreadsheet ID")
	}

	if config.spreadsheetId == "" {
		spreadsheet := &sheets.Spreadsheet{
			Properties: &sheets.SpreadsheetProperties{
				Title: "Hex to ASCII Conversion",
			},
		}

		resp, err := config.sheetsService.Spreadsheets.Create(spreadsheet).Do()
		if err != nil {
			return fmt.Errorf("unable to create spreadsheet: %v", err)
		}
		config.spreadsheetId = resp.SpreadsheetId
		fmt.Printf("Created new spreadsheet with ID: %s\n", config.spreadsheetId)
	}

	values := &sheets.ValueRange{
		Values: [][]interface{}{
			{fileName, base64.StdEncoding.EncodeToString([]byte(asciiStr))},
		},
	}

	_, err = config.sheetsService.Spreadsheets.Values.Append(
		config.spreadsheetId,
		"Sheet1!A1",
		values,
	).ValueInputOption("RAW").Do()

	if err != nil {
		return fmt.Errorf("unable to append data: %v", err)
	}

	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func HexToAscii(hexStr string) (string, error) {
	// Clean up the hex string
	hexStr = strings.ReplaceAll(hexStr, " ", "")
	hexStr = strings.ReplaceAll(hexStr, "\n", "")
	hexStr = strings.ReplaceAll(hexStr, "\r", "")

	asciiBytes, err := hex.DecodeString(hexStr)
	if err != nil {
		return "", err
	}

	return string(asciiBytes), nil
}

func convertFile(srcPath, dstPath string) error {
	hexData, err := ioutil.ReadFile(srcPath)
	if err != nil {
		return err
	}

	asciiStr, err := HexToAscii(string(hexData))
	if err != nil {
		return err
	}

	return ioutil.WriteFile(dstPath, []byte(asciiStr), 0644)
}
