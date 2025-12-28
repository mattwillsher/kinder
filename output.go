package main

import (
	"fmt"
	"os"
)

// Verbosity levels
const (
	VerbosityQuiet   = -1 // Only errors
	VerbosityDefault = 0  // Emoji + action summary
	VerbosityVerbose = 1  // Full details
)

// Global verbosity level
var verbosity = VerbosityDefault

// SetVerbosity sets the global verbosity level
func SetVerbosity(v int) {
	verbosity = v
}

// GetVerbosity returns the current verbosity level
func GetVerbosity() int {
	return verbosity
}

// IsVerbose returns true if verbose output is enabled
func IsVerbose() bool {
	return verbosity >= VerbosityVerbose
}

// IsQuiet returns true if quiet mode is enabled
func IsQuiet() bool {
	return verbosity <= VerbosityQuiet
}

// Output prints a message at the default verbosity level
func Output(format string, args ...interface{}) {
	if verbosity >= VerbosityDefault {
		fmt.Printf(format, args...)
	}
}

// OutputLn prints a message with newline at the default verbosity level
func OutputLn(args ...interface{}) {
	if verbosity >= VerbosityDefault {
		fmt.Println(args...)
	}
}

// Verbose prints a message only when verbose mode is enabled
func Verbose(format string, args ...interface{}) {
	if verbosity >= VerbosityVerbose {
		fmt.Printf(format, args...)
	}
}

// VerboseLn prints a message with newline only when verbose mode is enabled
func VerboseLn(args ...interface{}) {
	if verbosity >= VerbosityVerbose {
		fmt.Println(args...)
	}
}

// Error prints an error message (always shown unless quiet)
func Error(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format, args...)
}

// ErrorLn prints an error message with newline (always shown)
func ErrorLn(args ...interface{}) {
	fmt.Fprintln(os.Stderr, args...)
}

// Status prints a status line with emoji and message
// In default mode: "emoji message"
// In verbose mode: "emoji message (details)"
func Status(emoji, message, details string) {
	if verbosity < VerbosityDefault {
		return
	}

	if verbosity >= VerbosityVerbose && details != "" {
		fmt.Printf("%s %s (%s)\n", emoji, message, details)
	} else {
		fmt.Printf("%s %s\n", emoji, message)
	}
}

// StatusResult prints a result line with checkmark or warning
// In default mode: "  checkmark message"
// In verbose mode: "  checkmark message (details)"
func StatusResult(success bool, message, details string) {
	if verbosity < VerbosityDefault {
		return
	}

	icon := "✓"
	if !success {
		icon = "⚠️ "
	}

	if verbosity >= VerbosityVerbose && details != "" {
		fmt.Printf("  %s %s (%s)\n", icon, message, details)
	} else {
		fmt.Printf("  %s %s\n", icon, message)
	}
}

// Section prints a section header
func Section(emoji, title string) {
	if verbosity < VerbosityDefault {
		return
	}
	fmt.Printf("%s %s...\n", emoji, title)
}

// SectionDone prints completion of a section (only in verbose mode adds newline)
func SectionDone() {
	if verbosity >= VerbosityVerbose {
		fmt.Println()
	}
}

// BlankLine prints a blank line (only in default+ mode)
func BlankLine() {
	if verbosity >= VerbosityDefault {
		fmt.Println()
	}
}

// Header prints a header message
func Header(message string) {
	if verbosity >= VerbosityDefault {
		fmt.Println(message)
	}
}

// Success prints a success message with checkmark emoji
func Success(message string) {
	if verbosity >= VerbosityDefault {
		fmt.Printf("✅ %s\n", message)
	}
}

// Info prints an informational message (only in verbose mode)
func Info(format string, args ...interface{}) {
	if verbosity >= VerbosityVerbose {
		fmt.Printf("   "+format+"\n", args...)
	}
}

// ServiceInfo prints service availability info
func ServiceInfo(name, url string) {
	if verbosity >= VerbosityDefault {
		fmt.Printf("  - %s: %s\n", name, url)
	}
}

// ProgressStart prints a compact progress line for starting an action.
// In default mode: "  emoji name... " (no newline, waiting for ProgressDone)
// In verbose mode: uses Section format with newline
func ProgressStart(emoji, name string) {
	if verbosity < VerbosityDefault {
		return
	}

	if verbosity >= VerbosityVerbose {
		// Verbose mode: use existing Section format
		fmt.Printf("%s %s...\n", emoji, name)
	} else {
		// Default mode: compact inline format
		fmt.Printf("  %s %-16s", emoji, name)
	}
}

// ProgressDone completes a progress line with success/failure indicator.
// In default mode: prints "✓" or "✗" on the same line, with optional brief status
// In verbose mode: prints full status result
func ProgressDone(success bool, details string) {
	if verbosity < VerbosityDefault {
		return
	}

	if verbosity >= VerbosityVerbose {
		// Verbose mode: use existing StatusResult format
		icon := "✓"
		if !success {
			icon = "✗"
		}
		if details != "" {
			fmt.Printf("  %s %s\n", icon, details)
		} else {
			fmt.Printf("  %s Done\n", icon)
		}
	} else {
		// Default mode: compact inline completion with optional brief status
		if success {
			if details != "" {
				fmt.Printf("✓ %s\n", details)
			} else {
				fmt.Printf("✓\n")
			}
		} else {
			if details != "" {
				fmt.Printf("✗ %s\n", details)
			} else {
				fmt.Printf("✗\n")
			}
		}
	}
}

// ProgressSkip indicates an action was skipped (e.g., already exists)
func ProgressSkip(reason string) {
	if verbosity < VerbosityDefault {
		return
	}

	if verbosity >= VerbosityVerbose {
		fmt.Printf("  ○ %s\n", reason)
	} else {
		fmt.Printf("○ %s\n", reason)
	}
}
