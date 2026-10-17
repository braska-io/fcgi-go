<?php
// $_FILES flattened per part, next to $_POST and the parse warning.
require __DIR__ . '/_emit.php';
fixture('app/request/files');
$files = upload_entries();
emit([
    'post' => (object) $_POST,
    'count' => count($files),
    'files' => (object) $files,
    'warning' => last_warning(),
]);
