package reportingv1_test

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	reportingv1 "github.com/jonathanagustin/conduit/gen/go/conduit/reporting/v1"
	reporting "github.com/jonathanagustin/conduit/internal/reporting/domain"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// Native projection changes must be reflected in the staged RPC contract before
// an adapter can accidentally discard an availability, count or row field.
func TestOverviewContractMatchesNativeProjection(t *testing.T) {
	pairs := []struct {
		native  any
		message proto.Message
	}{
		{reporting.OverviewProjection{}, &reportingv1.GetOverviewResponse{}},
		{reporting.OverviewChangeRequest{}, &reportingv1.OverviewChangeRequest{}},
		{reporting.OverviewPlan{}, &reportingv1.OverviewPlan{}},
		{reporting.OverviewInstallation{}, &reportingv1.OverviewInstallation{}},
		{reporting.OverviewPromotion{}, &reportingv1.OverviewPromotion{}},
		{reporting.Panel[reporting.OverviewChangeRequest]{}, &reportingv1.ChangeRequestPanel{}},
		{reporting.Panel[reporting.OverviewPlan]{}, &reportingv1.PlanPanel{}},
		{reporting.Panel[reporting.OverviewInstallation]{}, &reportingv1.InstallationPanel{}},
		{reporting.Panel[struct{}]{}, &reportingv1.StaleReportPanel{}},
		{reporting.Panel[reporting.OverviewPromotion]{}, &reportingv1.PromotionPanel{}},
	}
	for _, pair := range pairs {
		source := reflect.TypeOf(pair.native)
		var native, wire []string
		for i := 0; i < source.NumField(); i++ {
			key := strings.Split(source.Field(i).Tag.Get("json"), ",")[0]
			if key != "" && key != "-" {
				native = append(native, key)
			}
		}
		fields := pair.message.ProtoReflect().Descriptor().Fields()
		for i := 0; i < fields.Len(); i++ {
			wire = append(wire, fields.Get(i).JSONName())
		}
		sort.Strings(native)
		sort.Strings(wire)
		if !reflect.DeepEqual(native, wire) {
			t.Errorf("%s fields: native=%v proto=%v", source, native, wire)
		}
	}
	panel := (&reportingv1.PlanPanel{}).ProtoReflect().Descriptor().Fields()
	if panel.ByName("status").Kind() != protoreflect.StringKind || panel.ByName("count").Kind() != protoreflect.Int64Kind || !panel.ByName("items").IsList() {
		t.Fatal("panel must preserve availability, integer count and bounded native items")
	}
}

func TestOverviewContractHasNoMutationOrStreamAuthority(t *testing.T) {
	service := reportingv1.File_conduit_reporting_v1_overview_proto.Services().ByName("ReportingService")
	if service.Methods().Len() != 1 {
		t.Fatal("review new methods against registered REST routes")
	}
	method := service.Methods().Get(0)
	if method.Name() != "GetOverview" || method.IsStreamingClient() || method.IsStreamingServer() {
		t.Fatal("overview contract is a unary read")
	}
	options := method.Options().(*descriptorpb.MethodOptions)
	if options.GetIdempotencyLevel() != descriptorpb.MethodOptions_NO_SIDE_EFFECTS {
		t.Fatal("overview cannot gain write authority")
	}
	if method.Input().Fields().Len() != 0 {
		t.Fatal("identity and authority must not be caller-selected body fields")
	}
}
