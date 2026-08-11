package queue

// CloneQueue returns a fully detached copy of q. It preserves nil fields and
// performs no serialization, validation, clock, identity, or filesystem work.
func CloneQueue(q *Queue) *Queue {
	if q == nil {
		return nil
	}
	out := *q
	out.FailedRecoveryReceiptID = clonePointer(q.FailedRecoveryReceiptID)
	if q.Groups != nil {
		out.Groups = make([]Group, len(q.Groups))
		for i := range q.Groups {
			out.Groups[i] = cloneQueueGroup(q.Groups[i])
		}
	}
	return &out
}

func cloneQueueGroup(group Group) Group {
	out := group
	out.StartedAt = clonePointer(group.StartedAt)
	out.CompletedAt = clonePointer(group.CompletedAt)
	if group.Items != nil {
		out.Items = make([]Item, len(group.Items))
		for i := range group.Items {
			out.Items[i] = cloneQueueItem(group.Items[i])
		}
	}
	return out
}

func cloneQueueItem(item Item) Item {
	out := item
	out.RunID = clonePointer(item.RunID)
	out.PreclaimTerminal = clonePointer(item.PreclaimTerminal)
	out.AppendedAt = clonePointer(item.AppendedAt)
	if item.TemplateParams != nil {
		out.TemplateParams = make(map[string]string, len(item.TemplateParams))
		for key, value := range item.TemplateParams {
			out.TemplateParams[key] = value
		}
	}
	return out
}

func clonePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
