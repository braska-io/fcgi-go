<?php
// Entry-point checks applications run before bootstrapping.
require __DIR__ . '/_json.php';
fixture('app/compat/entry');
$file = $_SERVER['SCRIPT_FILENAME'] ?? '';
$root = $_SERVER['DOCUMENT_ROOT'] ?? '';
emit_json([
    'script_name' => $_SERVER['SCRIPT_NAME'] ?? null,
    'filename_ends_with_script' => str_ends_with($file, '/app/compat/entry.php'),
    'filename_starts_with_document_root' => $root !== '' && str_starts_with($file, $root),
    'filename_is_this_file' => realpath($file) === __FILE__,
]);
