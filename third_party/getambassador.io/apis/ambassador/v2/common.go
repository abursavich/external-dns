// Copyright 2020 Datawire.  All rights reserved
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v2

import (
	"encoding/json"
)

// AmbassadorID declares which Ambassador instances should pay
// attention to this resource.  May either be a string or a list of
// strings.  If no value is provided, the default is:
//
//    ambassador_id:
//    - "default"
//
// +kubebuilder:validation:Type="d6e-union:string,array"
type AmbassadorID []string

func (aid *AmbassadorID) UnmarshalJSON(data []byte) error {
	return (*StringOrStringList)(aid).UnmarshalJSON(data)
}

// +kubebuilder:validation:Type="d6e-union:string,array"
type StringOrStringList []string

func (sl *StringOrStringList) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*sl = nil
		return nil
	}

	var err error
	var list []string
	var single string

	if err = json.Unmarshal(data, &single); err == nil {
		*sl = StringOrStringList([]string{single})
		return nil
	}

	if err = json.Unmarshal(data, &list); err == nil {
		*sl = StringOrStringList(list)
		return nil
	}

	return err
}
