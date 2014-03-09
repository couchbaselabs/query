//  Copyright (c) 2014 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

package expression

import (
	"github.com/couchbaselabs/query/value"
	json "github.com/dustin/gojson"
)

type DecodeJSON struct {
	unaryBase
}

func NewDecodeJSON(arg Expression) Function {
	return &DecodeJSON{
		unaryBase{
			operand: arg,
		},
	}
}

func (this *DecodeJSON) evaluate(arg value.Value) (value.Value, error) {
	if arg.Type() == value.MISSING {
		return value.MISSING_VALUE, nil
	} else if arg.Type() != value.STRING {
		return value.NULL_VALUE, nil
	}

	s := arg.Actual().(string)
	var p interface{}
	err := json.Unmarshal([]byte(s), &p)
	if err != nil {
		return value.NULL_VALUE, nil
	}

	return value.NewValue(p), nil
}

func (this *DecodeJSON) Constructor() FunctionConstructor {
	return func(args Expressions) Function {
		return NewDecodeJSON(args[0])
	}
}

type EncodeJSON struct {
	unaryBase
}

func NewEncodeJSON(arg Expression) Function {
	return &EncodeJSON{
		unaryBase{
			operand: arg,
		},
	}
}

func (this *EncodeJSON) evaluate(arg value.Value) (value.Value, error) {
	return value.NewValue(string(arg.Bytes())), nil
}

func (this *EncodeJSON) Constructor() FunctionConstructor {
	return func(args Expressions) Function {
		return NewEncodeJSON(args[0])
	}
}

type EncodedSize struct {
	unaryBase
}

func NewEncodedSize(arg Expression) Function {
	return &EncodedSize{
		unaryBase{
			operand: arg,
		},
	}
}

func (this *EncodedSize) evaluate(arg value.Value) (value.Value, error) {
	return value.NewValue(float64(len(arg.Bytes()))), nil
}

func (this *EncodedSize) Constructor() FunctionConstructor {
	return func(args Expressions) Function {
		return NewEncodedSize(args[0])
	}
}

type PolyLength struct {
	unaryBase
}

func NewPolyLength(arg Expression) Function {
	return &PolyLength{
		unaryBase{
			operand: arg,
		},
	}
}

func (this *PolyLength) evaluate(arg value.Value) (value.Value, error) {
	if arg.Type() == value.MISSING {
		return value.MISSING_VALUE, nil
	}

	switch oa := arg.Actual().(type) {
	case string:
		return value.NewValue(float64(len(oa))), nil
	case []interface{}:
		return value.NewValue(float64(len(oa))), nil
	case map[string]interface{}:
		return value.NewValue(float64(len(oa))), nil
	default:
		return value.NULL_VALUE, nil
	}
}

func (this *PolyLength) Constructor() FunctionConstructor {
	return func(args Expressions) Function {
		return NewPolyLength(args[0])
	}
}