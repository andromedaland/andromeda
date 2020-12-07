// Copyright 2020 William Perron. All rights reserved. MIT License.
package models

type File struct {
	Uid       string   `json:"uid,omitempty"`
	Specifier string   `json:"specifier,omitempty"`
	DependsOn []File   `json:"depends_on,omitempty"`
	DType     []string `json:"dgraph.type,omitempty"`
}