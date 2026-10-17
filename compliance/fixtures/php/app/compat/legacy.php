<?php
// A legacy front controller that reads its route from an injected _url
// query parameter.
require __DIR__ . '/_json.php';
fixture('app/compat/legacy');
emit_json(['get' => (object) $_GET]);
