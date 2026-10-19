# docs-site

Static documentation for `leroymerlin-cli`, built with Next.js and Fumadocs and
exported to `out/`.

The public domain is a placeholder for now: `https://leroymerlin-cli.example`.
Set the real address in `app/layout.tsx` when the site is deployed.

```bash
npm install
npm run dev       # http://localhost:3000/docs
npm run typecheck
npm run build     # static export in out/
npm start         # preview out/ on http://localhost:3000/docs
```

`npm install` runs `fumadocs-mdx` to generate the content types before the first
build. The preview command needs Python 3.

## Reference pages

`content/docs/reference/*.mdx` is written by a Go docs generator that reads the
cobra command tree. Edit the command definitions, not the generated pages.

`content/docs/reference/index.mdx` is hand-written and is not replaced by the
generator.
