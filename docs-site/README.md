# docs-site

Static documentation for `leroymerlin-cli`, built with Next.js and Fumadocs and
exported to `out/`.

It is published to GitHub Pages at `https://n0nuser.github.io/leroymerlin-cli/`.
The project-site prefix is `basePath` in `next.config.mjs`; keep it in step with
the repository name.

```bash
npm install
npm run dev       # http://localhost:3000/leroymerlin-cli/docs
npm run typecheck
npm run build     # static export in out/
npm start         # preview out/ on http://localhost:3000/leroymerlin-cli/docs
```

`npm install` runs `fumadocs-mdx` to generate the content types before the first
build. The preview command needs Python 3.

## Reference pages

`content/docs/reference/*.mdx` is written by a Go docs generator that reads the
cobra command tree. Edit the command definitions, not the generated pages.

`content/docs/reference/index.mdx` is hand-written and is not replaced by the
generator.
