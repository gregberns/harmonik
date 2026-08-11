package queue

import "testing"

func TestBuildProposedQueueDetachesRequestValues(t *testing.T) {
	request := ValidationRequest{
		Groups:           []Group{{Items: []Item{{TemplateParams: map[string]string{"new": "fixed"}}}}},
		ActiveQueue:      &Queue{Groups: []Group{{Items: []Item{{TemplateParams: map[string]string{"old": "fixed"}}}}}},
		IsAppend:         true,
		AppendGroupIndex: 0,
	}
	proposed := buildProposedQueue(request)
	proposed.Groups[0].Items[0].TemplateParams["old"] = "changed"
	proposed.Groups[0].Items[1].TemplateParams["new"] = "changed"

	if request.ActiveQueue.Groups[0].Items[0].TemplateParams["old"] != "fixed" {
		t.Fatal("append proposal changed the active queue")
	}
	if request.Groups[0].Items[0].TemplateParams["new"] != "fixed" {
		t.Fatal("append proposal changed request groups")
	}
}
