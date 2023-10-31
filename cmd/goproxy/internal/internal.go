package internal

// Execute executes the goproxy command and returns exit code.
func Execute() int {
	if err := newGoproxyCmd().Execute(); err != nil {
		return 1
	}
	return 0
}
