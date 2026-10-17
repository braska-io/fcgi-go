<?php
// getallheaders() sorted by name, and whether apache_request_headers()
// exists and agrees with it.
require __DIR__ . '/_emit.php';
fixture('app/request/headers');
$all = getallheaders();
ksort($all);
$apache = function_exists('apache_request_headers') ? apache_request_headers() : null;
if ($apache !== null) {
    ksort($apache);
}
emit([
    'headers' => (object) $all,
    'apache_request_headers' => $apache === null ? 'missing' : ($apache === $all ? 'same' : 'different'),
]);
