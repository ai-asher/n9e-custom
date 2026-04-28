// Package suppress implements alert inhibition:
// when a root-cause alert fires, automatically suppress its derived alerts
// based on source/target label matching (Alertmanager-style inhibit_rules).
package suppress
