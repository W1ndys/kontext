package schema

import "testing"

func TestModuleContractValidateRequiresOwns(t *testing.T) {
	t.Parallel()

	contract := ModuleContract{
		Module: ModuleInfo{
			Name:    "updater",
			Path:    "internal/updater",
			Purpose: "test",
		},
	}

	if err := contract.Validate(); err == nil {
		t.Fatal("expected owns to be required")
	}
}

func TestModuleContractValidateRejectsEmptyOwnsItem(t *testing.T) {
	t.Parallel()

	contract := ModuleContract{
		Module: ModuleInfo{
			Name:    "updater",
			Path:    "internal/updater",
			Purpose: "test",
		},
		Owns: []string{""},
	}

	if err := contract.Validate(); err == nil {
		t.Fatal("expected empty owns item to be rejected")
	}
}

func TestModuleContractValidateAcceptsNonEmptyOwns(t *testing.T) {
	t.Parallel()

	contract := ModuleContract{
		Module: ModuleInfo{
			Name:    "updater",
			Path:    "internal/updater",
			Purpose: "test",
		},
		Owns: []string{"负责更新契约文件"},
	}

	if err := contract.Validate(); err != nil {
		t.Fatalf("unexpected validate error: %v", err)
	}
}
