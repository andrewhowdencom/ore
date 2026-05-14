package session

import (
	"testing"

	"github.com/andrewhowdencom/ore/thread"
	"github.com/stretchr/testify/assert"
)

func TestSession_Getters(t *testing.T) {
	thr, _ := thread.NewMemoryStore().Create()
	sess := &Session{
		id:     thr.ID,
		thread: thr,
	}

	assert.Equal(t, thr.ID, sess.ID())
	assert.Equal(t, thr, sess.Thread())
}
