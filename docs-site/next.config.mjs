import { createMDX } from 'fumadocs-mdx/next';

const withMDX = createMDX();

// GitHub Pages serves a project site under /<repo>/, so the export has to know
// that prefix. Keep this in step with the repository name.
const basePath = '/leroymerlin-cli';

/** @type {import('next').NextConfig} */
const config = {
  reactStrictMode: true,
  output: 'export',
  trailingSlash: true,
  basePath,
  assetPrefix: basePath,
  images: { unoptimized: true },
};

export default withMDX(config);
