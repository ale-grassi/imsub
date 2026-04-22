package bot

import (
	"fmt"
	"os"
	"testing"

	"imsub/internal/platform/i18n"
)

func TestMain(m *testing.M) {
	if err := i18n.Ensure(); err != nil {
		fmt.Fprintln(os.Stderr, "i18n.Ensure:", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
