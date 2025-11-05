package os

import (
	"context"
	"strings"

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/joomcode/errorx"
)

// DaemonReload reloads the systemd manager configuration.
// It is equivalent to running "systemctl daemon-reload".
func DaemonReload(ctx context.Context) error {
	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return ErrSystemdConnection.Wrap(err, "failed to connect to systemd")
	}
	defer conn.Close()

	if err := conn.ReloadContext(ctx); err != nil {
		return ErrSystemdOperation.Wrap(err, "daemon-reload failed")
	}
	return nil
}

// EnableService enables the specified service.
// It is equivalent to running "systemctl enable <service>".
// The service name can be provided with or without the .service suffix.
func EnableService(ctx context.Context, name string) error {
	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return ErrSystemdConnection.Wrap(err, "failed to connect to systemd")
	}
	defer conn.Close()

	// Ensure the service name has the .service suffix
	serviceName := ensureServiceSuffix(name)

	// The second parameter 'false' means not to enable for runtime only, but rather persistently.
	// The third parameter 'true' means to force overwrite existing symlinks.
	_, _, err = conn.EnableUnitFilesContext(ctx, []string{serviceName}, false, true)
	if err != nil {
		return ErrSystemdOperation.Wrap(err, "failed to enable service %s", serviceName).
			WithProperty(errorx.RegisterProperty("service"), serviceName)
	}

	return nil
}

// DisableService disables the specified service.
// It is equivalent to running "systemctl disable <service>".
// The service name can be provided with or without the .service suffix.
func DisableService(ctx context.Context, name string) error {
	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return ErrSystemdConnection.Wrap(err, "failed to connect to systemd")
	}
	defer conn.Close()

	// Ensure the service name has the .service suffix
	serviceName := ensureServiceSuffix(name)

	// The second parameter 'false' means not to disable for runtime only, but rather persistently.
	_, err = conn.DisableUnitFilesContext(ctx, []string{serviceName}, false)
	if err != nil {
		return ErrSystemdOperation.Wrap(err, "failed to disable service %s", serviceName).
			WithProperty(errorx.RegisterProperty("service"), serviceName)
	}

	return nil
}

// IsServiceEnabled checks if the specified service is enabled.
// The service name can be provided with or without the .service suffix.
func IsServiceEnabled(ctx context.Context, name string) (bool, error) {
	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()

	// Ensure the service name has the .service suffix
	serviceName := ensureServiceSuffix(name)

	// List unit files matching this name
	unitFiles, err := conn.ListUnitFilesByPatternsContext(ctx, []string{}, []string{serviceName})
	if err != nil {
		return false, err
	}

	if len(unitFiles) == 0 {
		return false, nil
	}

	// Check if the unit file is in enabled state
	return unitFiles[0].Type == "enabled", nil
}

// RestartService starts the specified service.
// This function waits until the service is fully started.
// It is equivalent to running "systemctl start <service>".
// The service name can be provided with or without the .service suffix.
func RestartService(ctx context.Context, name string) error {
	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return ErrSystemdConnection.Wrap(err, "failed to connect to systemd")
	}
	defer conn.Close()

	// Ensure the service name has the .service suffix
	serviceName := ensureServiceSuffix(name)

	// Make this call synchronous and wait until the unit is started.
	jobChan := make(chan string, 1) // buffered channel to avoid goroutine leaks

	// The second parameter 'replace' means to replace any existing job for the unit.
	_, err = conn.RestartUnitContext(ctx, serviceName, "replace", jobChan)
	if err != nil {
		return ErrSystemdOperation.Wrap(err, "failed to start service %s", serviceName).
			WithProperty(errorx.RegisterProperty("service"), serviceName)
	}

	select {
	case result := <-jobChan:
		if result != "done" {
			return ErrSystemdOperation.New("service %s start failed: %s", serviceName, result).
				WithProperty(errorx.RegisterProperty("service"), serviceName).
				WithProperty(errorx.RegisterProperty("job_result"), result)
		}
		return nil

	case <-ctx.Done():
		return ErrSystemdOperation.Wrap(ctx.Err(), "timeout waiting for service %s to start", serviceName).
			WithProperty(errorx.RegisterProperty("service"), serviceName)
	}
}

// StopService stops the specified service.
// This function waits until the service is fully stopped.
// It is equivalent to running "systemctl stop <service>".
// The service name can be provided with or without the .service suffix.
func StopService(ctx context.Context, name string) error {
	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return ErrSystemdConnection.Wrap(err, "failed to connect to systemd")
	}
	defer conn.Close()

	// Ensure the service name has the .service suffix
	serviceName := ensureServiceSuffix(name)

	// Make this call synchronous and wait until the unit is stopped.
	jobChan := make(chan string, 1) // buffered channel to avoid goroutine leaks

	// The second parameter 'replace' means to replace any existing job for the unit.
	_, err = conn.StopUnitContext(ctx, serviceName, "replace", jobChan)
	if err != nil {
		return ErrSystemdOperation.Wrap(err, "failed to stop service %s", serviceName).
			WithProperty(errorx.RegisterProperty("service"), serviceName)
	}

	select {
	case result := <-jobChan:
		if result != "done" {
			return ErrSystemdOperation.New("service %s stop failed: %s", serviceName, result).
				WithProperty(errorx.RegisterProperty("service"), serviceName).
				WithProperty(errorx.RegisterProperty("job_result"), result)
		}
		return nil

	case <-ctx.Done():
		return ErrSystemdOperation.Wrap(ctx.Err(), "timeout waiting for service %s to stop", serviceName).
			WithProperty(errorx.RegisterProperty("service"), serviceName)
	}
}

// IsServiceRunning checks if the specified service is running.
// The service name can be provided with or without the .service suffix.
func IsServiceRunning(ctx context.Context, name string) (bool, error) {
	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return false, ErrSystemdConnection.Wrap(err, "failed to connect to systemd")
	}
	defer conn.Close()

	// Ensure the service name has the .service suffix
	serviceName := ensureServiceSuffix(name)

	props, err := conn.GetUnitPropertiesContext(ctx, serviceName)
	if err != nil {
		return false, err
	}

	return props["ActiveState"] == "active", nil
}

// ensureServiceSuffix ensures the service name has the .service suffix.
// If the name already has the suffix, it returns it unchanged.
func ensureServiceSuffix(name string) string {
	if !strings.HasSuffix(name, ".service") {
		return name + ".service"
	}
	return name
}

// MaskUnit masks the specified unit, preventing systemd from activating it.
// It is equivalent to running "systemctl mask <unit>".
// The unit name can be provided with or without the appropriate suffix (.service, .swap, etc.).
func MaskUnit(ctx context.Context, name string) error {
	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return ErrSystemdConnection.Wrap(err, "failed to connect to systemd")
	}
	defer conn.Close()

	// The second parameter 'false' means not to mask for runtime only, but rather persistently.
	// The third parameter 'true' means to force overwrite existing symlinks.
	_, err = conn.MaskUnitFilesContext(ctx, []string{name}, false, true)
	if err != nil {
		return ErrSystemdOperation.Wrap(err, "failed to mask unit %s", name).
			WithProperty(errorx.RegisterProperty("unit"), name)
	}

	return nil
}

// UnmaskUnit unmasks the specified unit, allowing systemd to activate it again.
// It is equivalent to running "systemctl unmask <unit>".
// The unit name can be provided with or without the appropriate suffix (.service, .swap, etc.).
func UnmaskUnit(ctx context.Context, name string) error {
	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return ErrSystemdConnection.Wrap(err, "failed to connect to systemd")
	}
	defer conn.Close()

	// The second parameter 'false' means not to unmask for runtime only, but rather persistently.
	_, err = conn.UnmaskUnitFilesContext(ctx, []string{name}, false)
	if err != nil {
		return ErrSystemdOperation.Wrap(err, "failed to unmask unit %s", name).
			WithProperty(errorx.RegisterProperty("unit"), name)
	}

	return nil
}
