package handler

func init() {
	RegisterFeature(
		"access-ready",
		"partitioned",
		"pluggable-storage-engines",
		"reshard",
		"scheduler",
	)
}
