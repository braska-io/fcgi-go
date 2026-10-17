<?php
// An upload handler: what PHP kept of a body above post_max_size.
require __DIR__ . '/_json.php';
fixture('app/compat/upload');
emit_json([
    'content_length' => $_SERVER['CONTENT_LENGTH'] ?? null,
    'post' => (object) $_POST,
    'files' => (object) $_FILES,
]);
