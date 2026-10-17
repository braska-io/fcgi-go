<?php
// A front controller that routes on the raw REQUEST_URI.
require __DIR__ . '/_json.php';
fixture('app/compat/front');
emit_json(['request_uri' => $_SERVER['REQUEST_URI'] ?? null]);
