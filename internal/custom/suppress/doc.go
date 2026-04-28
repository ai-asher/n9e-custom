// Package suppress implements cross-rule alert inhibition (Alertmanager-style).
//
// Design summary
//
//	When an alert matching SourceMatch is currently firing, any incoming
//	alert matching TargetMatch is suppressed iff every label in EqualLabels
//	holds the same value on the source and target events.
//
//	  e.g. SourceMatch={alertname=HostDown}
//	       TargetMatch={category=service}
//	       EqualLabels=[host]
//	     → "HostDown on db01" suppresses every service alert with host=db01.
//
// Two-faced hook
//
//	A single hook does BOTH jobs in one pass:
//	  1. If the event matches any rule's SourceMatch — register it as a live
//	     root cause and let it through (it must reach senders so operators
//	     are paged about the actual outage).
//	  2. If the event matches any rule's TargetMatch — consult the live
//	     root-cause index; suppress when an active source shares the
//	     equal-labels with this event.
//	The two cases are NOT mutually exclusive — a single event may both
//	register itself as a root cause AND be suppressed by an even-higher-tier
//	root cause that fired earlier.
//
// Concurrency model
//
//	Single in-process index protected by RWMutex. Multi-center deployments
//	will need a shared backing store later; the RootCauseProvider interface
//	is the seam where that swap happens (today's impl is a memory provider).
package suppress
