package state

// Locker takes the single lock mutating commands hold. ok=false means
// another holder has it.
type Locker interface {
	TryLock(path string) (unlock func(), ok bool, err error)
}

// FakeLocker is an in-memory Locker for tests.
type FakeLocker struct {
	Held   bool // another run holds the lock
	Locked bool // we hold it now
	Taken  int  // successful TryLock calls
}

func (f *FakeLocker) TryLock(string) (func(), bool, error) {
	if f.Held || f.Locked {
		return nil, false, nil
	}
	f.Locked = true
	f.Taken++
	return func() { f.Locked = false }, true, nil
}
