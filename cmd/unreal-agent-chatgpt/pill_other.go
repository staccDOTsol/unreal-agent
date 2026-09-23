//go:build !darwin

package main

import "context"

func startPill(system) {}

func watchPill(context.Context, func(string) string) error { return nil }
