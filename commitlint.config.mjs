export default {
  extends: ['@commitlint/config-conventional'],
  rules: {
    // Allow subjects that start with acronyms/proper nouns (e.g. "feat: TLS
    // 1.3 support"); the default rejects anything it classifies as
    // sentence-/start-/upper-case.
    'subject-case': [0],
  },
};
