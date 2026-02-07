HERALD_BINARY=heraldev
HERALDCTL_BINARY=heraldctldev
COVERAGE_MIN=95.0
COVERAGE_PROFILE=coverage.out

.PHONY: build release test clean

build: test
	go build -o $(HERALD_BINARY) ./cmd/herald
	go build -o $(HERALDCTL_BINARY) ./cmd/heraldctl

release: test
	go build -ldflags="-s -w" -o herald ./cmd/herald
	go build -ldflags="-s -w" -o heraldctl ./cmd/heraldctl

test:
	@out_file=$$(mktemp); \
	if ! go test ./... -coverprofile=$(COVERAGE_PROFILE) > $$out_file 2>&1; then \
		cat $$out_file; \
		rm -f $$out_file; \
		exit 1; \
	fi; \
	cat $$out_file; \
	awk -v min="$(COVERAGE_MIN)" '/^ok[[:space:]]+/ && index($$0, "coverage: ") > 0 { \
		pkg=$$2; \
		split($$0, parts, "coverage: "); \
		covtxt=parts[2]; \
		sub(/%.*/, "", covtxt); \
		cov=covtxt+0; \
		printf("Package coverage %s: %.1f%% (min %.1f%%)\n", pkg, cov, min); \
		if (cov < min) { \
			printf("Coverage gate failed for %s: %.1f%% < %.1f%%\n", pkg, cov, min) > "/dev/stderr"; \
			bad=1; \
		} \
		found=1; \
	} END { \
		if (!found) { \
			print "Coverage gate failed: no per-package coverage lines found" > "/dev/stderr"; \
			bad=1; \
		} \
		exit bad; \
	}' $$out_file; \
	rm -f $$out_file; \
	go tool cover -func=$(COVERAGE_PROFILE) | awk -v min="$(COVERAGE_MIN)" '/^total:/ { \
		gsub(/%/, "", $$3); \
		cov=$$3+0; \
		printf("Total coverage: %.1f%% (min %.1f%%)\n", cov, min); \
		if (cov < min) { \
			printf("Coverage gate failed: %.1f%% < %.1f%%\n", cov, min) > "/dev/stderr"; \
			exit 1; \
		} \
	}'

clean:
	rm -f $(HERALD_BINARY) $(HERALDCTL_BINARY) $(COVERAGE_PROFILE)
