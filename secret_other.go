//go:build !windows

package main

// shortcut: stores secrets unencrypted off Windows, where the app only runs for development and tests;
// use the OS keychain here if the app ever ships for macOS or Linux.
func protect(plain []byte) ([]byte, error) { return plain, nil }

func unprotect(sealed []byte) ([]byte, error) { return sealed, nil }
