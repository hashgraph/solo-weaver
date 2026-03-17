// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"fmt"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// FlagDefinition defines a command-line flag typed by T.
type FlagDefinition[T any] struct {
	Name        string
	ShortName   string
	Description string
	Default     T
}

// Clone returns an independent copy of the descriptor.
// Useful when you need a local variant with a different default or description.
func (fp FlagDefinition[T]) Clone() FlagDefinition[T] {
	return FlagDefinition[T]{
		Name:        fp.Name,
		ShortName:   fp.ShortName,
		Description: fp.Description,
		Default:     fp.Default,
	}
}

// valueFrom contains the common type-switch logic to extract a value
// from the provided pflag.FlagSet.
func (fp FlagDefinition[T]) valueFrom(flags *pflag.FlagSet) (T, error) {
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

// Value resolves a flag value using the following precedence:
//  1. Local (non-persistent) flags on this command
//  2. Own persistent flags on this command (defined via SetVarP on this command directly)
//  3. Persistent flags inherited from parent/root commands (merged by Cobra via ParseFlags)
//
// Use ValueLocal() or ValueOwnPersistent() if you need strict single-scope semantics.
func (fp FlagDefinition[T]) Value(cmd *cobra.Command, args []string) (T, error) {
	var zero T
	if cmd == nil {
		return zero, errorx.IllegalArgument.New("command cannot be nil")
	}

	if args == nil {
		args = []string{}
	}

	// Step 1: try local flags (cmd.Flags() — also includes inherited persistent
	// after Cobra's mergePersistentFlags fires inside ParseFlags)
	if val, err := fp.ValueLocal(cmd, args); err == nil {
		return val, nil
	}

	// Step 2: fallback to own persistent flags (flags registered with SetVarP
	// directly on this command that may not appear in the merged cmd.Flags())
	if val, err := fp.ValueOwnPersistent(cmd, args); err == nil {
		return val, nil
	}

	return zero, fmt.Errorf("flag %s not found in local, own-persistent, or inherited flag sets", fp.Name)
}

func (fp FlagDefinition[T]) ValueLocal(cmd *cobra.Command, args []string) (T, error) {
	if args == nil {
		args = []string{}
	}

	if err := cmd.ParseFlags(args); err != nil {
		var zero T
		return zero, errorx.InternalError.Wrap(err, "failed to parse flags for command %s", cmd.Name())
	}

	return fp.valueFrom(cmd.Flags())
}

// ValueOwnPersistent reads the value of a persistent flag defined directly on this
// command (via SetVarP). It does NOT search ancestor/parent commands.
//
// Use Value() if you want the full resolution chain (local → own persistent → inherited persistent).
// Use ValueLocal() if you want only local (non-persistent) flags on this command.
func (fp FlagDefinition[T]) ValueOwnPersistent(cmd *cobra.Command, args []string) (T, error) {
	if args == nil {
		args = []string{}
	}
	if err := cmd.ParseFlags(args); err != nil {
		var zero T
		return zero, errorx.InternalError.Wrap(err, "failed to parse flags for command %s", cmd.Name())
	}
	return fp.valueFrom(cmd.PersistentFlags())
}

// SetVarP sets up the persistent flag and exits on error.
func (fp FlagDefinition[T]) SetVarP(cmd *cobra.Command, p *T, required bool) {
	if err := fp.varP(cmd, p, required); err != nil {
		doctor.CheckErr(context.Background(), err, "failed to set flag %s", fp.Name)
	}
}

// SetVar sets up the non-persistent flag and exits on error.
func (fp FlagDefinition[T]) SetVar(cmd *cobra.Command, p *T, required bool) {
	if err := fp.varNP(cmd, p, required); err != nil {
		doctor.CheckErr(context.Background(), err, "failed to set flag %s", fp.Name)
	}
}

// varP sets up a persistent flag (kept for tests/compat) by delegating to setFlagVar.
func (fp FlagDefinition[T]) varP(cmd *cobra.Command, p *T, required bool) error {
	err := fp.setFlagVar(cmd.PersistentFlags(), cmd, p)
	if err != nil {
		return err
	}

	return fp.MarkRequiredP(cmd, required)
}

// varNP sets up a non-persistent flag by delegating to setFlagVar.
func (fp FlagDefinition[T]) varNP(cmd *cobra.Command, p *T, required bool) error {
	err := fp.setFlagVar(cmd.Flags(), cmd, p)
	if err != nil {
		return err
	}

	return fp.MarkRequired(cmd, required)
}

// setFlagVar contains the common registration logic and is used to set up both persistent and non-persistent flags.
func (fp FlagDefinition[T]) setFlagVar(flags *pflag.FlagSet, cmd *cobra.Command, p *T) error {
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
		var def []string
		if any(fp.Default) != nil {
			def = any(fp.Default).([]string)
		} else {
			def = []string{}
		}
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

func (fp FlagDefinition[T]) MarkRequired(cmd *cobra.Command, v bool) error {
	if v {
		err := cmd.MarkFlagRequired(fp.Name)
		if err != nil {
			return errorx.InternalError.Wrap(err, "failed to mark flag %s as required", fp.Name)
		}
	}

	return nil
}

func (fp FlagDefinition[T]) MarkRequiredP(cmd *cobra.Command, v bool) error {
	if v {
		err := cmd.MarkPersistentFlagRequired(fp.Name)
		if err != nil {
			return errorx.InternalError.Wrap(err, "failed to mark persistent flag %s as required", fp.Name)
		}
	}

	return nil
}

// GetExecutionMode determines the execution mode based on the provided flags.
// It ensures that only one of the flags is set; otherwise, it returns an error.
// The precedence is as follows:
// 1. continueOnErr
// 2. rollbackOnErr
// 3. stopOnErr (default)
func GetExecutionMode(continueOnErr bool, stopOnErr bool, rollbackOnErr bool) (automa.TypeMode, error) {
	// validate only one flag is set
	count := 0
	if continueOnErr {
		count++
	}
	if stopOnErr {
		count++
	}
	if rollbackOnErr {
		count++
	}

	if count > 1 {
		return automa.StopOnError, errorx.IllegalArgument.New("only one of execution mode can be set; "+
			"found continue-on-error: %t, stop-on-error: %t, rollback-on-error: %t", continueOnErr, stopOnErr, rollbackOnErr)
	}

	// determine execution mode
	if continueOnErr {
		return automa.ContinueOnError, nil
	} else if rollbackOnErr {
		return automa.RollbackOnError, nil
	} else {
		return automa.StopOnError, nil
	}
}

// RootFlags contains the flags registered on the root command and available
// to every subcommand via persistent inheritance.
type RootFlags struct {
	Config             string
	Force              bool
	SkipHardwareChecks bool
	LogLevel           string
}

// ExtractRootFlags extracts the flags registered on the root command.
// It also calls InitConfig to load configuration and initialise logging.
func ExtractRootFlags(cmd *cobra.Command, args []string, flags *RootFlags) error {
	var err error

	flags.Config, err = FlagConfig().Value(cmd, args)
	if err != nil {
		return errorx.IllegalArgument.Wrap(err, "failed to get config flag")
	}

	flags.Force, err = FlagForce().Value(cmd, args)
	if err != nil {
		return errorx.IllegalArgument.Wrap(err, "failed to get force flag")
	}

	flags.SkipHardwareChecks, err = FlagSkipHardwareChecks().Value(cmd, args)
	if err != nil {
		return errorx.IllegalArgument.Wrap(err, "failed to get skip-hardware-checks flag")
	}

	flags.LogLevel, err = FlagLogLevel().Value(cmd, args)
	if err != nil {
		return errorx.IllegalArgument.Wrap(err, "failed to get log-level flag")
	}

	InitConfig(cmd.Context(), flags)

	logx.As().Info().Any("root-flags", flags).Msg("Extracted root command flags")
	return nil
}

// InitConfig loads the configuration file and initialises the logger.
func InitConfig(ctx context.Context, flags *RootFlags) {
	var err error
	err = config.Initialize(flags.Config)
	if err != nil {
		doctor.CheckErr(ctx, err)
	}

	logConfig := config.Get().Log
	if flags.LogLevel != "" {
		logConfig.Level = flags.LogLevel // override log level if flag is set
	}

	err = logx.Initialize(logConfig)
	if err != nil {
		doctor.CheckErr(ctx, err)
	}
}

func DetectShortNameCollisions(root *cobra.Command) bool {
	found := false

	var walk func(cmd *cobra.Command, inherited map[string]string)
	walk = func(cmd *cobra.Command, inherited map[string]string) {
		// Collect this command's own flags (local + persistent)
		own := map[string]string{}

		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			if f.Shorthand == "" {
				return
			}
			key := cmd.Name() + ":" + f.Name
			if existing, ok := own[f.Shorthand]; ok {
				// two flags on same command share a shorthand
				found = true
				logx.As().Warn().Msgf("flag short name '-%s' collision on same command: %s vs %s", f.Shorthand, existing, key)
			}
			own[f.Shorthand] = key
		})

		cmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
			if f.Shorthand == "" {
				return
			}
			key := cmd.Name() + "(persistent):" + f.Name
			if existing, ok := own[f.Shorthand]; ok {
				found = true
				logx.As().Warn().Msgf("flag short name '-%s' collision on same command: %s vs %s", f.Shorthand, existing, key)
			}
			own[f.Shorthand] = key
		})

		// Check own flags against what is inherited from ancestors
		for short, ownKey := range own {
			if inheritedKey, ok := inherited[short]; ok {
				found = true
				logx.As().Warn().Msgf("flag short name '-%s' collision with inherited flag: %s shadows %s", short, ownKey, inheritedKey)
			}
		}

		// Build inherited map for children: add this command's persistent flags
		childInherited := make(map[string]string, len(inherited))
		for k, v := range inherited {
			childInherited[k] = v
		}
		cmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
			if f.Shorthand != "" {
				childInherited[f.Shorthand] = cmd.Name() + "(persistent):" + f.Name
			}
		})

		for _, c := range cmd.Commands() {
			walk(c, childInherited)
		}
	}

	walk(root, map[string]string{})
	return found
}
