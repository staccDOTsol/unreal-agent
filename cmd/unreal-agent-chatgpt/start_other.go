//go:build !darwin && !windows

package main

func notify(string) {}

func notifyError(error) {}
