package store

import "testing"

func TestValidateKind(t *testing.T) {
	valid := []string{"flight", "stay", "activity", "transport", "food", "other"}
	for _, kind := range valid {
		if err := ValidateKind(kind); err != nil {
			t.Fatalf("ValidateKind(%q) returned %v", kind, err)
		}
	}
	if err := ValidateKind("visa"); err == nil {
		t.Fatal("expected invalid kind to fail")
	}
}

func TestValidateRouteOptionValues(t *testing.T) {
	for _, routeType := range []string{"direct_flight", "flight_train", "train", "bus", "other"} {
		if err := ValidateRouteType(routeType); err != nil {
			t.Fatalf("route type %q should be valid", routeType)
		}
	}
	if err := ValidateRouteType("ferry"); err == nil {
		t.Fatal("expected invalid route type to fail")
	}
	for _, status := range []string{"considering", "shortlisted", "booked", "dismissed"} {
		if err := ValidateRouteStatus(status); err != nil {
			t.Fatalf("status %q should be valid", status)
		}
	}
	if err := ValidateRouteStatus("maybe"); err == nil {
		t.Fatal("expected invalid route status to fail")
	}
}
