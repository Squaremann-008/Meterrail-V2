// eslint-config-next 16 ships native flat configs, so these are spread
// directly. The FlatCompat shim that older setups needed is not used here — it
// cannot serialise the new config objects and throws on a circular structure.
import coreWebVitals from 'eslint-config-next/core-web-vitals';
import typescript from 'eslint-config-next/typescript';

const config = [
  {
    ignores: ['.next/**', 'node_modules/**', 'next-env.d.ts', 'public/**'],
  },

  ...coreWebVitals,
  ...typescript,

  {
    rules: {
      // A leading underscore is the conventional "deliberately unused" marker.
      '@typescript-eslint/no-unused-vars': [
        'error',
        { argsIgnorePattern: '^_', varsIgnorePattern: '^_', caughtErrorsIgnorePattern: '^_' },
      ],
      // Deliberate `any` should be argued for in review, not silently allowed.
      '@typescript-eslint/no-explicit-any': 'error',
    },
  },
];

export default config;
