<?php
// A JSON API endpoint: the declared length against what php://input holds.
// The body need not be valid JSON; only the lengths matter.
require __DIR__ . '/_json.php';
fixture('app/compat/input');
$input = file_get_contents('php://input');
emit_json([
    'content_type' => $_SERVER['CONTENT_TYPE'] ?? null,
    'content_length' => $_SERVER['CONTENT_LENGTH'] ?? null,
    'input_length' => strlen($input),
]);
