package model

type IteratorOptions struct {
	Skip     int
	Limit    int
	StartKey []byte
	EndKey   []byte

	SkipDeleted   bool
	SkipDesignDoc bool
	SkipLocalDoc  bool

	CleanKey func([]byte) string
	KeyFunc  func(k []byte) []byte

	BucketName []byte
}
