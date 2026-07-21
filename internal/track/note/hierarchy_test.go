package note

import (
	"reflect"
	"testing"
)

func TestUpTargets(t *testing.T) {
	props := []Prop{
		{Key: "up", Value: "Parent", Type: TypeLink},
		{Key: "up", Value: "draft", Type: TypeString}, // not a link, not a parent
		{Key: "owner", Value: "Ada", Type: TypeLink},  // different key
		{Key: "up", Value: "Other", Type: TypeLink},
		{Key: "up", Value: "Parent", Type: TypeLink}, // duplicate
	}
	got := UpTargets(props)
	if want := []string{"Parent", "Other"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("UpTargets = %v, want %v", got, want)
	}
}
