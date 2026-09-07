// Lint config for the GNOME Shell extension sources.
//
// Not part of the installed extension — it exists so `npx eslint .` can catch
// the class of mistake that a GNOME session would otherwise only reveal at
// runtime, in a log, after a re-login: a misspelled identifier. The globals
// below are the ones GJS and gnome-shell inject into every extension module.

export default [
    {
        files: ['**/*.js'],
        languageOptions: {
            ecmaVersion: 2022,
            sourceType: 'module',
            globals: {
                // Injected by gnome-shell into extension modules.
                global: 'readonly',
                // GJS built-ins.
                console: 'readonly',
                TextDecoder: 'readonly',
                TextEncoder: 'readonly',
                imports: 'readonly',
                log: 'readonly',
                logError: 'readonly',
            },
        },
        rules: {
            // The reason this config exists: a typo'd identifier in a Shell
            // extension surfaces only as a silent no-op in the journal.
            'no-undef': 'error',
            'no-redeclare': 'error',
            'no-dupe-keys': 'error',
            'no-unreachable': 'error',
            'no-constant-condition': 'error',
            // GJS uses these for GObject-ish shapes and unused catch bindings
            // are the normal way to ignore an expected failure.
            'no-unused-vars': ['error', {
                args: 'after-used',
                argsIgnorePattern: '^_',
                caughtErrors: 'none',
            }],
        },
    },
];
