// Conventional Commits linting. Used by the CI job in .github/workflows/ci.yml
// (@commitlint/config-conventional). See CONTRIBUTING.md.
export default {
  extends: ['@commitlint/config-conventional'],
  rules: {
    // Allow long lines in the body (e.g. Co-Authored-By footers, URLs).
    'body-max-line-length': [0, 'always'],
  },
};
