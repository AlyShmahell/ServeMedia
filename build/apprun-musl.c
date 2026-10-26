#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

/* AppImage entry for the musl build. quick-sharun must not deploy this
   program's target: the musl servemedia ELF stays host-linked. */
int main(int argc, char **argv) {
	const char *appdir = getenv("APPDIR");
	char root[4096];
	char bin[4096];
	char **args;
	int i, n;

	if (appdir == NULL || appdir[0] == '\0') {
		fprintf(stderr, "servemedia: APPDIR is unset\n");
		return 127;
	}
	unsetenv("LD_LIBRARY_PATH");
	unsetenv("LD_PRELOAD");

	n = snprintf(root, sizeof root, "%s/servemedia/.local/share/servemedia", appdir);
	if (n < 0 || (size_t)n >= sizeof root) {
		return 127;
	}
	n = snprintf(bin, sizeof bin, "%s/servemedia/.local/bin/servemedia", appdir);
	if (n < 0 || (size_t)n >= sizeof bin) {
		return 127;
	}
	if (setenv("SERVEMEDIA_ROOT", root, 1) != 0) {
		return 127;
	}
	if (setenv("SERVEMEDIA_LAUNCHED", "1", 1) != 0) {
		return 127;
	}

	args = calloc((size_t)argc + 2, sizeof *args);
	if (args == NULL) {
		return 127;
	}
	args[0] = bin;
	args[1] = "--webkit";
	for (i = 1; i < argc; i++) {
		args[i + 1] = argv[i];
	}
	execv(bin, args);
	perror("servemedia");
	return 127;
}
