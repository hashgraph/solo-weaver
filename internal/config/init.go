// SPDX-License-Identifier: Apache-2.0

package config

// NOTE: Logging is no longer initialized here. The logx package's own init()
// provides a default console logger at import time. The real configuration
// (console vs file, TUI suppression) happens in root.go's initConfig() after
// cobra parses flags. Calling logx.Initialize() here with ConsoleLogging:true
// would create a window where console output leaks before the TUI can suppress it.
