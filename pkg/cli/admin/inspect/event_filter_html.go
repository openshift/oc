package inspect

const eventHTMLPage = `
<!doctype html>
<html lang="en">
  <head>
    <!-- Required meta tags -->
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1, shrink-to-fit=no">

    <!-- Bootstrap CSS -->
    <link rel="stylesheet" href="https://stackpath.bootstrapcdn.com/bootstrap/4.5.0/css/bootstrap.min.css" integrity="sha384-9aIt2nRpC12Uk9gS9baDl411NQApFmC26EwAOH8WgZl5MYYxFfc+NcPb1dKGj7Sk" crossorigin="anonymous">
	<link href="https://unpkg.com/bootstrap-table@1.16.0/dist/bootstrap-table.min.css" rel="stylesheet">

    <title>Events</title>
	<style>
      html {
       font-size: 12px;
      }
    </style>
  </head>
  <body>

<table
  class="table table-bordered table-hover table-sm"
  data-toggle="table"
  data-search="true"
  data-pagination="true"
  data-page-size="100"
  data-show-columns-toggle-all="true"
  data-show-pagination-switch="true"
  data-show-columns="true">
  <thead>
    <tr>
      <th>Time</th>
      <th data-sortable="true">Namespace</th>
      <th data-sortable="true">Component</th>
      <th>Reason</th>
      <th data-escape="true">Message</th>
    </tr>
  </thead>
  <tbody>
    {{range .Items}}
    <tr>
      <td>{{formatTime .LastTimestamp}}</td>
      <td>{{.Namespace}}</td>
      <td>{{.Source.Component}}</td>
      <td>{{formatReason .Reason}}</td>
      <td data-formatter="messageFormatter">{{.Message}}</td>
    </tr>
    {{end}}
  </tbody>
</table>
	<script>
	function messageFormatter(value, row) {
    	return '<code>'+value+'</code>'
  	}
	</script>
    <!-- Optional JavaScript -->
    <!-- jQuery first, then Popper.js, then Bootstrap JS -->
    <script src="https://code.jquery.com/jquery-3.5.1.slim.min.js" integrity="sha384-DfXdz2htPH0lsSSs5nCTpuj/zy4C+OGpamoFVy38MVBnE+IbbVYUew+OrCXaRkfj" crossorigin="anonymous"></script>
    <script src="https://cdn.jsdelivr.net/npm/popper.js@1.16.0/dist/umd/popper.min.js" integrity="sha384-Q6E9RHvbIyZFJoft+2mJbHaEWldlvI9IOYy5n3zV9zzTtmI3UksdQRVvoxMfooAo" crossorigin="anonymous"></script>
    <script src="https://stackpath.bootstrapcdn.com/bootstrap/4.5.0/js/bootstrap.min.js" integrity="sha384-OgVRvuATP1z7JjHLkuOU7Xw704+h835Lr+6QL9UvYjZE3Ipu6Tp75j7Bh/kR0JKI" crossorigin="anonymous"></script>
	<script src="https://unpkg.com/bootstrap-table@1.16.0/dist/bootstrap-table.min.js"></script>
  </body>
</html>
`
