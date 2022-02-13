package metrics

import (
	"reflect"
	"testing"

	"github.com/goccy/go-json"

	"github.com/vmihailenco/msgpack/v5"
)

type unmarshal func([]byte, interface{}) error

func Test_Metric_Unmarshal(t *testing.T) {
	tests := []struct {
		name        string
		unmarshaler unmarshal
		payload     []byte
		want        Metric
	}{
		{"json",
			json.Unmarshal,
			[]byte(`{"name":"test","value":123,"labels": ["test1", "test2"]}`),
			Metric{
				Name:   "test",
				Value:  123,
				Labels: []string{"test1", "test2"},
			},
		},
		{
			"msgpack",
			msgpack.Unmarshal,
			// {"name":"test","value":123,"labels": ["test1", "test2"]}
			[]byte{0x83, 0xa4, 0x6e, 0x61, 0x6d, 0x65, 0xa4, 0x74, 0x65, 0x73, 0x74, 0xa5, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x7b, 0xa6, 0x6c, 0x61, 0x62, 0x65, 0x6c, 0x73, 0x92, 0xa5, 0x74, 0x65, 0x73, 0x74, 0x31, 0xa5, 0x74, 0x65, 0x73, 0x74, 0x32},
			Metric{
				Name:   "test",
				Value:  123,
				Labels: []string{"test1", "test2"},
			},
		},
		{
			"msgpack pascal case",
			msgpack.Unmarshal,
			// {"Name":"test","Value":123,"Labels": ["test1", "test2"]}
			[]byte{0x83, 0xa4, 0x4e, 0x61, 0x6d, 0x65, 0xa4, 0x74, 0x65, 0x73, 0x74, 0xa5, 0x56, 0x61, 0x6c, 0x75, 0x65, 0x7b, 0xa6, 0x4c, 0x61, 0x62, 0x65, 0x6c, 0x73, 0x92, 0xa5, 0x74, 0x65, 0x73, 0x74, 0x31, 0xa5, 0x74, 0x65, 0x73, 0x74, 0x32},
			Metric{
				Name:   "test",
				Value:  123,
				Labels: []string{"test1", "test2"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Metric{}
			err := test.unmarshaler(test.payload, &got)
			if err != nil {
				t.Error(err)
				return
			}
			if !reflect.DeepEqual(test.want, got) {
				t.Errorf("Want: %v, Got: %v", test.want, got)
			}
		})
	}
}
