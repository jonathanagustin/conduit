package auditpostgres

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestAuditPageDescendingSeeksRecordedSequenceWithFilters(t *testing.T) {
	database, _ := databaseForTest(t)
	epoch := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for index := range 6 {
		event := eventForTest(fmt.Sprintf("event-%d", index))
		event.CreatedAt = epoch.Add(-time.Duration(index) * time.Minute)
		if index == 3 {
			event.ResourceRID = "other-resource"
		}
		appendEvent(t, database, event)
	}
	query := PageQuery{Resource: "resource", Since: epoch.Add(-5 * time.Minute), Until: epoch.Add(-time.Minute), Limit: 2, Desc: true}
	first, total := pageForTest(t, database, query)
	if want := []string{"event-5", "event-4"}; !reflect.DeepEqual(rowIDs(first), want) || total != 3 {
		t.Fatalf("first descending page = %v total %d, want %v total 3", rowIDs(first), total, want)
	}
	query.AfterSequence = first[len(first)-1].Sequence
	second, total := pageForTest(t, database, query)
	if want := []string{"event-2"}; !reflect.DeepEqual(rowIDs(second), want) || total != 3 {
		t.Fatalf("descending seek = %v total %d, want %v total 3", rowIDs(second), total, want)
	}
	query.Desc, query.AfterSequence = false, 0
	ascending, _ := pageForTest(t, database, query)
	if want := []string{"event-2", "event-4"}; !reflect.DeepEqual(rowIDs(ascending), want) {
		t.Fatalf("default ascending behavior changed: %v", rowIDs(ascending))
	}
}
