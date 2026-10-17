<?php
// $_GET exactly as PHP parsed QUERY_STRING.
require __DIR__ . '/_emit.php';
fixture('app/request/get');
emit((object) $_GET);
