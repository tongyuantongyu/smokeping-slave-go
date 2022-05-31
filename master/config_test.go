package master

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"testing"
)

func TestParseConfig(t *testing.T) {
	data, err := ioutil.ReadFile("_test.sjson")
	assert.Nil(t, err, "can't open test file")

	c, err := parseRawConfig(data)
	assert.Nil(t, err, "failed parsing")

	ag := aggregateTargets(&c.Targets, "node5h")
	fmt.Println(ag)
}
