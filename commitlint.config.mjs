export default {
  extends: ['@commitlint/config-conventional'],
  rules: {
    // Allow subjects that start with acronyms/proper nouns (e.g. "feat: AWS
    // SigV4 gateway"); the default rejects anything it classifies as
    // sentence-/start-/upper-case.
    'subject-case': [0],
  },
};
