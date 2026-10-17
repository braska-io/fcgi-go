<?php
// $_REQUEST and the two directives that decide its order.
require __DIR__ . '/_emit.php';
fixture('app/request/request');
emit([
    'request_order' => (string) ini_get('request_order'),
    'variables_order' => (string) ini_get('variables_order'),
    'request' => (object) $_REQUEST,
]);
