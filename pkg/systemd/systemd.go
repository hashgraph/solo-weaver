package systemd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/coreos/go-systemd/v22/dbus"
)

// DaemonReload reloads the systemd manager configuration.
// It is equivalent to running "systemctl daemon-reload".
func DaemonReload(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()

	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return fmt.Errorf("connect to systemd: %w", err)
	}
	defer conn.Close()

	if err := conn.ReloadContext(ctx); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	return nil
}

// EnableService enables the specified service.
// It is equivalent to running "systemctl enable <service>".
// The service name can be provided with or without the .service suffix.
func EnableService(parent context.Context, name string) error {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()

	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return fmt.Errorf("connect to systemd: %w", err)
	}
	defer conn.Close()

	// Ensure the service name has the .service suffix
	serviceName := ensureServiceSuffix(name)

	// The second parameter 'false' means not to enable for runtime only, but rather persistently.
	// The third parameter 'true' means to force overwrite existing symlinks.
	_, _, err = conn.EnableUnitFilesContext(ctx, []string{serviceName}, false, true)
	if err != nil {
		return fmt.Errorf("enable service %s: %w", serviceName, err)
	}

	return nil
}

// DisableService disables the specified service.
// It is equivalent to running "systemctl disable <service>".
// The service name can be provided with or without the .service suffix.
func DisableService(parent context.Context, name string) error {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()

	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return fmt.Errorf("connect to systemd: %w", err)
	}
	defer conn.Close()

	// Ensure the service name has the .service suffix
	serviceName := ensureServiceSuffix(name)

	// The second parameter 'false' means not to disable for runtime only, but rather persistently.
	_, err = conn.DisableUnitFilesContext(ctx, []string{serviceName}, false)
	if err != nil {
		return fmt.Errorf("disable service %s: %w", serviceName, err)
	}

	return nil
}

// StartService starts the specified service.
// This function waits until the service is fully started.
// It is equivalent to running "systemctl start <service>".
// The service name can be provided with or without the .service suffix.
func StartService(parent context.Context, name string) error {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()

	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return fmt.Errorf("connect to systemd: %w", err)
	}
	defer conn.Close()

	// Ensure the service name has the .service suffix
	serviceName := ensureServiceSuffix(name)

	// Make this call synchronous and wait until the unit is started.
	jobChan := make(chan string, 1) // buffered channel to avoid goroutine leaks

	// The second parameter 'replace' means to replace any existing job for the unit.
	_, err = conn.StartUnitContext(ctx, serviceName, "replace", jobChan)
	if err != nil {
		return fmt.Errorf("start service %s: %w", serviceName, err)
	}

	select {
	case result := <-jobChan:
		if result != "done" {
			return fmt.Errorf("service %s start failed: %s", serviceName, result)
		}
		return nil

	case <-ctx.Done():
		return fmt.Errorf("timeout waiting for service %s to start: %w", serviceName, ctx.Err())
	}
}

// StopService stops the specified service.
// This function waits until the service is fully stopped.
// It is equivalent to running "systemctl stop <service>".
// The service name can be provided with or without the .service suffix.
func StopService(parent context.Context, name string) error {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()

	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return fmt.Errorf("connect to systemd: %w", err)
	}
	defer conn.Close()

	// Ensure the service name has the .service suffix
	serviceName := ensureServiceSuffix(name)

	// Make this call synchronous and wait until the unit is stopped.
	jobChan := make(chan string, 1) // buffered channel to avoid goroutine leaks

	// The second parameter 'replace' means to replace any existing job for the unit.
	_, err = conn.StopUnitContext(ctx, serviceName, "replace", jobChan)
	if err != nil {
		return fmt.Errorf("stop service %s: %w", serviceName, err)
	}

	select {
	case result := <-jobChan:
		if result != "done" {
			return fmt.Errorf("service %s stop failed: %s", serviceName, result)
		}
		return nil

	case <-ctx.Done():
		return fmt.Errorf("timeout waiting for service %s to stop: %w", serviceName, ctx.Err())
	}
}

// ensureServiceSuffix ensures the service name has the .service suffix.
// If the name already has the suffix, it returns it unchanged.
func ensureServiceSuffix(name string) string {
	if !strings.HasSuffix(name, ".service") {
		return name + ".service"
	}
	return name
}
