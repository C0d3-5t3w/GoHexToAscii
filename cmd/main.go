package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

func main() {
	// Setup signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create channel for interrupt signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Handle interrupts in a separate goroutine
	go func() {
		<-sigChan
		fmt.Println("\nReceived interrupt signal. Shutting down...")
		cancel()
	}()

	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Enter source folder path containing hex files: ")
	srcFolder, _ := reader.ReadString('\n')
	srcFolder = strings.TrimSpace(srcFolder)

	fmt.Print("Enter destination folder path for ASCII files: ")
	dstFolder, _ := reader.ReadString('\n')
	dstFolder = strings.TrimSpace(dstFolder)

	// Create destination folder if it doesn't exist
	if err := os.MkdirAll(dstFolder, 0755); err != nil {
		fmt.Printf("Error creating destination folder: %v\n", err)
		return
	}

	// Update processFiles call to use context
	processFiles(ctx, srcFolder, dstFolder)
}

func processFiles(ctx context.Context, srcFolder, dstFolder string) {
	files, err := ioutil.ReadDir(srcFolder)
	if err != nil {
		fmt.Printf("Error reading source folder: %v\n", err)
		return
	}

	for _, file := range files {
		// Check for context cancellation
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
		dstPath := filepath.Join(dstFolder, strings.TrimSuffix(file.Name(), filepath.Ext(file.Name()))+".txt")

		if fileExists(dstPath) {
			fmt.Printf("Skipping %s: Already converted\n", file.Name())
			continue
		}

		if err := convertFile(srcPath, dstPath); err != nil {
			fmt.Printf("Error converting %s: %v\n", file.Name(), err)
			continue
		}

		fmt.Printf("Converted %s successfully\n", file.Name())
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

// HexToAscii converts a hex string to ASCII string
func HexToAscii(hexStr string) (string, error) {
	// Clean up the hex string
	hexStr = strings.ReplaceAll(hexStr, " ", "")
	hexStr = strings.ReplaceAll(hexStr, "\n", "")
	hexStr = strings.ReplaceAll(hexStr, "\r", "")

	// Decode hex to bytes
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
