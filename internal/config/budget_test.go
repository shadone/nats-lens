package config

import "testing"

func TestQueryBudget(t *testing.T) {
	t.Setenv(PasswordEnv, "")
	cfg, err := Parse("natslens", nil)
	if err != nil {
		t.Fatal(err)
	}
	for threads, want := range map[int]int{12: 4, 6: 2, 2: 1, 1: 1} {
		if got := cfg.QueryBudgetFor(threads); got != want {
			t.Errorf("derived budget for %d threads = %d, want %d", threads, got, want)
		}
	}
	if cfg, err = Parse("natslens", []string{"-config", writeFile(t, "query_budget: 6\n")}); err != nil || cfg.QueryBudgetFor(12) != 6 {
		t.Fatalf("file: %d %v", cfg.QueryBudget, err)
	}
	cfg, err = Parse("natslens", []string{"-config", writeFile(t, "query_budget: -1\n")})
	if err != nil || cfg.QueryBudgetFor(12) != 0 {
		t.Fatalf("query_budget -1 turns the budget off: %d %v", cfg.QueryBudgetFor(12), err)
	}
	if _, err := Parse("natslens", []string{"-config", writeFile(t, "query_budget: -2\n")}); err == nil {
		t.Fatal("query_budget below -1 must be rejected")
	}
}
