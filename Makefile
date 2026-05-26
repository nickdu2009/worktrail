PORT ?= 3000

.PHONY: docs-manual-serve
docs-manual-serve:
	npx docsify-cli serve docs/manual -p $(PORT)
