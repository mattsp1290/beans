package issue

import "testing"

func TestTypesConfigValidType(t *testing.T) {
	t.Parallel()
	def := DefaultTypesConfig()
	for _, typ := range DefaultTypes {
		if !def.ValidType(typ) {
			t.Errorf("%q should be valid by default", typ)
		}
	}
	if def.ValidType("story") {
		t.Error("story is not a default type")
	}
	custom := TypesConfig{Names: []string{"story"}}
	if !custom.ValidType("story") || custom.ValidType("task") {
		t.Errorf("custom vocabulary not applied: %+v", custom)
	}
	if !(TypesConfig{}).ValidType("bug") {
		t.Error("an empty config falls back to the defaults")
	}
}
