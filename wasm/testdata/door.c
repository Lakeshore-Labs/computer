// door.c is a guest of wasi-python's door: it calls host::call with "ping" and prints what it is given back, or
// the call's status. With the argument null its host_alloc answers 0, as malloc does when the heap is full.
// Build: clang --target=wasm32-wasip1 --sysroot=<wasi-sdk>/share/wasi-sysroot -Os -s -o door.wasm door.c
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

__attribute__((import_module("host"), import_name("call"))) int host_call(const char *, int, char **, int *);

static int null_alloc;

__attribute__((export_name("host_alloc"))) void *host_alloc(int n) { return null_alloc ? 0 : malloc(n); }

int main(int argc, char **argv) {
	null_alloc = argc > 1 && strcmp(argv[1], "null") == 0;
	char *reply = 0;
	int n = 0;
	int status = host_call("ping", 4, &reply, &n);
	if (status != 0) {
		printf("status %d\n", status);
		return 0;
	}
	printf("%.*s\n", n, reply);
	return 0;
}
