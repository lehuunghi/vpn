//go:build !windows

package main

func platformInit() error                         { return nil }
func platformCommand(args []string) (bool, error) { return false, nil }
