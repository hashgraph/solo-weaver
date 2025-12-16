package common

import (
	"context"
	"fmt"
	"time"

	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Examples of typed flag definitions
var (
	FlagProfile = FlagDefinition[string]{
		Name:        "profile",
		ShortName:   "p",
		Description: fmt.Sprintf("Deployment profiles %s", core.AllProfiles()),
		Default:     "",
	}

	FlagValuesFile = FlagDefinition[string]{
		Name:        "values",
		ShortName:   "f",
		Description: "Path to custom values file for installation",
		Default:     "",
	}

	FlagMetricsServer = FlagDefinition[bool]{
		Name:        "metrics-server",
		ShortName:   "m",
		Description: "Install Metrics Server",
		Default:     true,
	}
)

// FlagDefinition defines a command-line flag typed by T.
type FlagDefinition[T any] struct {
	Name        string
	ShortName   string
	Description string
	Default     T
}

// valueFrom contains the common type-switch logic to extract a value
// from the provided pflag.FlagSet.
func (fp *FlagDefinition[T]) valueFrom(flags *pflag.FlagSet) (T, error) {
	var zero T
	switch any(zero).(type) {
	case string:
		v, err := flags.GetString(fp.Name)
		if err != nil {
			return zero, err
		}
		return any(v).(T), nil
	case bool:
		v, err := flags.GetBool(fp.Name)
		if err != nil {
			return zero, err
		}
		return any(v).(T), nil
	case int:
		v, err := flags.GetInt(fp.Name)
		if err != nil {
			return zero, err
		}
		return any(v).(T), nil
	case int64:
		v, err := flags.GetInt64(fp.Name)
		if err != nil {
			return zero, err
		}
		return any(v).(T), nil
	case uint:
		v, err := flags.GetUint(fp.Name)
		if err != nil {
			return zero, err
		}
		return any(v).(T), nil
	case uint64:
		v, err := flags.GetUint64(fp.Name)
		if err != nil {
			return zero, err
		}
		return any(v).(T), nil
	case []string:
		v, err := flags.GetStringSlice(fp.Name)
		if err != nil {
			return zero, err
		}
		return any(v).(T), nil
	case time.Duration:
		v, err := flags.GetDuration(fp.Name)
		if err != nil {
			return zero, err
		}
		return any(v).(T), nil
	default:
		return zero, fmt.Errorf("unsupported flag type: %T", zero)
	}
}

// Value extracts the flag value (from the full flag set: persistent, non-persistent or from parent) of the provided cobra command.
// This is the preferred method to get flag values.
func (fp *FlagDefinition[T]) Value(cmd *cobra.Command, args []string) (T, error) {
	if args == nil {
		args = []string{}
	}

	// Parse the flags to ensure they are up to date such that it also retrieves from parent commands.
	err := cmd.ParseFlags(args)
	if err != nil {
		var zero T
		return zero, errorx.InternalError.Wrap(err, "failed to parse flags for command %s", cmd.Name())
	}

	return fp.valueFrom(cmd.Flags())
}

// ValueP extracts the persistent flag value from the provided cobra command.
// It won't look into persistent flags from parent commands. So use Value() instead.
func (fp *FlagDefinition[T]) ValueP(cmd *cobra.Command, args []string) (T, error) {
	if args == nil {
		args = []string{}
	}

	// Parse the flags to ensure they are up to date such that it also retrieves from parent commands.
	err := cmd.ParseFlags(args)
	if err != nil {
		var zero T
		return zero, errorx.InternalError.Wrap(err, "failed to parse flags for command %s", cmd.Name())
	}

	return fp.valueFrom(cmd.PersistentFlags())
}

// SetVarP sets up the persistent flag and exits on error.
func (fp *FlagDefinition[T]) SetVarP(cmd *cobra.Command, p *T, required bool) {
	if err := fp.varP(cmd, p, required); err != nil {
		doctor.CheckErr(context.Background(), err, "failed to set flag %s", fp.Name)
	}
}

// SetVar sets up the non-persistent flag and exits on error.
func (fp *FlagDefinition[T]) SetVar(cmd *cobra.Command, p *T, required bool) {
	if err := fp.varNP(cmd, p, required); err != nil {
		doctor.CheckErr(context.Background(), err, "failed to set flag %s", fp.Name)
	}
}

// varP sets up a persistent flag (kept for tests/compat) by delegating to setFlagVar.
func (fp *FlagDefinition[T]) varP(cmd *cobra.Command, p *T, required bool) error {
	err := fp.setFlagVar(cmd.PersistentFlags(), cmd, p)
	if err != nil {
		return err
	}

	return fp.MarkRequiredP(cmd, required)
}

// varNP sets up a non-persistent flag by delegating to setFlagVar.
func (fp *FlagDefinition[T]) varNP(cmd *cobra.Command, p *T, required bool) error {
	err := fp.setFlagVar(cmd.Flags(), cmd, p)
	if err != nil {
		return err
	}

	return fp.MarkRequired(cmd, required)
}

// setFlagVar contains the common registration logic and is used to set up both persistent and non-persistent flags.
func (fp *FlagDefinition[T]) setFlagVar(flags *pflag.FlagSet, cmd *cobra.Command, p *T) error {
	if p == nil {
		return errorx.IllegalArgument.New("pointer for flag %s is nil", fp.Name)
	}
	if cmd == nil {
		return errorx.IllegalArgument.New("command for flag %s is nil", fp.Name)
	}

	var zero T
	switch any(zero).(type) {
	case string:
		def := any(fp.Default).(string)
		ps, ok := any(p).(*string)
		if !ok {
			return errorx.IllegalArgument.New("expected *string for flag %s", fp.Name)
		}
		flags.StringVarP(ps, fp.Name, fp.ShortName, def, fp.Description)

	case bool:
		def := any(fp.Default).(bool)
		pb, ok := any(p).(*bool)
		if !ok {
			return errorx.IllegalArgument.New("expected *bool for flag %s", fp.Name)
		}
		flags.BoolVarP(pb, fp.Name, fp.ShortName, def, fp.Description)

	case int:
		def := any(fp.Default).(int)
		pi, ok := any(p).(*int)
		if !ok {
			return errorx.IllegalArgument.New("expected *int for flag %s", fp.Name)
		}
		flags.IntVarP(pi, fp.Name, fp.ShortName, def, fp.Description)

	case int64:
		def := any(fp.Default).(int64)
		pi64, ok := any(p).(*int64)
		if !ok {
			return errorx.IllegalArgument.New("expected *int64 for flag %s", fp.Name)
		}
		flags.Int64VarP(pi64, fp.Name, fp.ShortName, def, fp.Description)

	case uint:
		def := any(fp.Default).(uint)
		pu, ok := any(p).(*uint)
		if !ok {
			return errorx.IllegalArgument.New("expected *uint for flag %s", fp.Name)
		}
		flags.UintVarP(pu, fp.Name, fp.ShortName, def, fp.Description)

	case uint64:
		def := any(fp.Default).(uint64)
		pu64, ok := any(p).(*uint64)
		if !ok {
			return errorx.IllegalArgument.New("expected *uint64 for flag %s", fp.Name)
		}
		flags.Uint64VarP(pu64, fp.Name, fp.ShortName, def, fp.Description)

	case []string:
		def := any(fp.Default).([]string)
		pss, ok := any(p).(*[]string)
		if !ok {
			return errorx.IllegalArgument.New("expected *[]string for flag %s", fp.Name)
		}
		flags.StringSliceVarP(pss, fp.Name, fp.ShortName, def, fp.Description)

	case time.Duration:
		def := any(fp.Default).(time.Duration)
		pd, ok := any(p).(*time.Duration)
		if !ok {
			return errorx.IllegalArgument.New("expected *time.Duration for flag %s", fp.Name)
		}
		flags.DurationVarP(pd, fp.Name, fp.ShortName, def, fp.Description)

	default:
		return fmt.Errorf("unsupported flag type: %T", zero)
	}

	return nil
}

func (fp *FlagDefinition[T]) MarkRequired(cmd *cobra.Command, v bool) error {
	if v {
		err := cmd.MarkFlagRequired(fp.Name)
		if err != nil {
			return errorx.InternalError.Wrap(err, "failed to mark flag %s as required", fp.Name)
		}
	}

	return nil
}

func (fp *FlagDefinition[T]) MarkRequiredP(cmd *cobra.Command, v bool) error {
	if v {
		err := cmd.MarkPersistentFlagRequired(fp.Name)
		if err != nil {
			return errorx.InternalError.Wrap(err, "failed to mark persistent flag %s as required", fp.Name)
		}
	}

	return nil
}
