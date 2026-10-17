<?php
// How many variables PHP kept per superglobal, and the warning it raised.
require __DIR__ . '/_emit.php';
fixture('app/request/limits');
emit([
    'get' => count($_GET),
    'post' => count($_POST),
    'cookie' => count($_COOKIE),
    'warning' => last_warning(),
]);
