package main

import "context"

type timer interface {
	GetUserTime() int64
	GetKernelTime() int64
	GetRealTime() int64
	Run(context.Context, string, ...string) error
	Reset()
}

var processTimer timer
