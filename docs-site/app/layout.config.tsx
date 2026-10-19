import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';

export const baseOptions: BaseLayoutProps = {
  nav: {
    title: (
      <span className="inline-flex items-center gap-2 font-semibold">
        <img
          src="/leroymerlin-cli-logo.svg"
          alt="leroymerlin-cli"
          width={174}
          height={36}
          className="h-9 w-auto"
        />
      </span>
    ),
  },
  githubUrl: 'https://github.com/n0nuser/leroymerlin-cli',
};
