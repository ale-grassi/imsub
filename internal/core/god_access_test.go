package core

import "testing"

func TestGodAccessChecker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "nil receiver",
			run: func(t *testing.T) {
				t.Helper()
				var checker *GodAccessChecker
				if checker.IsGodTelegramUser(7) {
					t.Fatal("nil checker matched user, want false")
				}
			},
		},
		{
			name: "zero ids ignored",
			run: func(t *testing.T) {
				t.Helper()
				checker := NewGodAccessChecker(0, 7)
				if !checker.IsGodTelegramUser(7) || checker.IsGodTelegramUser(0) {
					t.Fatalf("checker match state unexpected: user7=%v user0=%v", checker.IsGodTelegramUser(7), checker.IsGodTelegramUser(0))
				}
			},
		},
		{
			name: "with ids on nil",
			run: func(t *testing.T) {
				t.Helper()
				var checker *GodAccessChecker
				checker = checker.WithIDs(7, 9)
				if !checker.IsGodTelegramUser(7) || !checker.IsGodTelegramUser(9) {
					t.Fatal("WithIDs on nil checker did not populate ids")
				}
			},
		},
		{
			name: "with ids keeps original immutable",
			run: func(t *testing.T) {
				t.Helper()
				orig := NewGodAccessChecker(7)
				next := orig.WithIDs(9, 7)
				if !orig.IsGodTelegramUser(7) || orig.IsGodTelegramUser(9) {
					t.Fatal("original checker mutated unexpectedly")
				}
				if !next.IsGodTelegramUser(7) || !next.IsGodTelegramUser(9) {
					t.Fatal("next checker missing expected ids")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}
