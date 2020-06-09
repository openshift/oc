package inspect

var eventFilterHtml = []byte(`
<!DOCTYPE html>
<html lang="en">
   <head>
      <!-- The link to the CSS that the grid needs -->
      <link rel="stylesheet" type="text/css" href="https://cdnjs.cloudflare.com/ajax/libs/jqueryui/1.12.1/themes/redmond/jquery-ui.min.css">
      <link rel="stylesheet" type="text/css" href="https://cdnjs.cloudflare.com/ajax/libs/font-awesome/4.7.0/css/font-awesome.min.css">
      <link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/free-jqgrid/4.15.5/css/ui.jqgrid.min.css">
      <script type="text/javascript" src="https://cdnjs.cloudflare.com/ajax/libs/jquery/3.1.1/jquery.min.js"></script>
      <script type="text/javascript" src="https://cdnjs.cloudflare.com/ajax/libs/jqueryui/1.12.1/jquery-ui.min.js"></script>
      <script type="text/javascript" src="https://cdnjs.cloudflare.com/ajax/libs/free-jqgrid/4.15.5/jquery.jqgrid.min.js"></script>
      <meta charset="utf-8" />
      <title>Openshift Event Grid Viewer</title>
   </head>
   <body>
      <script type="text/javascript" src="all-events.json.js"></script>
      <style type="text/css">
         /* set the size of the datepicker search control for Order Date*/
         #ui-datepicker-div { font-size:11px; }
         /* set the size of the autocomplete search control*/
         .ui-menu-item {
         }
         .ui-autocomplete {
         font-size: 11px;
         }       
         .myAltRowClass { background-color: #DDDDDC; background-image: none; }
      </style>
      <table id="jqGrid"></table>
      <div id="jqGridPager"></div>
      <script type="text/javascript"> 
         var _theGrid;
         $(document).ready(function () {
         var filter;
         _theGrid = $("#jqGrid").jqGrid({
         datatype: "jsonstring",
         datastr: allEvents,
             altRows: true,
             altclass: "myAltRowClass",
             jsonReader: {
                 root: "items"
             },
             colModel: [{
                     label: "Type",
                     name: "type",
                     key: false,
                     width: 60,
                     colmenu: true,
                     searchoptions: {
                         searchOperMenu: false,
                     }
                 }, {
                     label: "Component",
                     //sorttype: "integer",
                     name: "source.component",
                     key: false,
                     width: 220,
                     colmenu: true,
                     searchoptions: {
                         searchOperMenu: false,
                         sopt: ["cn"]
                     }
                 },
                 {
                     label: "Reason",
                     //sorttype: "integer",
                     name: "reason",
                     key: false,
                     width: 165,
                     colmenu: true,
                     searchoptions: {
                         searchOperMenu: false,
                         sopt: ["cn"]
                     }
                 },
                 {
                     label: "Message",
                     name: "message",
                     width: 635,
                     hidedlg: true,
                     // stype defines the search type control - in this case HTML select (dropdownlist)
                     // searchoptions value - name values pairs for the dropdown - they will appear as options
                     searchoptions: {
                         searchOperMenu: false,
                         sopt: ["cn"]
                     }
                 },
                 {
                     label: "firstTimestamp",
                     name: "firstTimestamp",
                     width: 150,
                     stype: "text",
                     searchoptions: {
                         searchOperMenu: false,
                         sopt: ["eq", "gt", "lt", "ge", "le"]
                     }
                 }, {
                     label: "lastTimestamp",
                     name: "lastTimestamp",
                     width: 150,
                     stype: "text",
                     searchoptions: {
                         searchOperMenu: false,
                         sopt: ["eq", "gt", "lt", "ge", "le"]
                     }
                 },
                 {
                     label: "involvedObject",
                     name: "involvedObject.name",
                     width: 260,
                     searchoptions: {
                         // dataInit is the client-side event that fires upon initializing the toolbar search field for a column
                         // use it to place a third party control to customize the toolbar
                         searchOperMenu: false,
                         sopt: ["cn"]
                     }
                 },
                 {
                     label: "Count",
                     sorttype: "number",
                     name: "count",
                     width: 100,
                     sopt: ["eq", "gt", "lt", "ge", "le"]
                 },
                 {
                     label: "id",
                     name: "metadata.uid",
                     key: true,
                     width: 220,
                     colmenu: true,
                     searchoptions: {
                         searchOperMenu: false,
                     }
                 }
             ],
             //loadonce: true,
             viewrecords: true,
             width: 1780,
             height: "auto",
             rowNum: 100,
             colMenu: true,
             shrinkToFit: false,
             pager: "#jqGridPager"
         });
         // activate the toolbar searching
         $("#jqGrid").jqGrid("filterToolbar", {
             // JSON stringify all data from search, including search toolbar operators
             stringResult: true,
             // instuct the grid toolbar to show the search options
             searchOperators: true
         });
         // activate the toolbar searching
         $("#jqGrid").jqGrid("navGrid", "#jqGridPager", {
             search: true, // show search button on the toolbar
             add: false,
             edit: false,
             del: false,
             refresh: true
         }, {}, {}, {}, {
             multipleSearch: true
             //stringResult : false
             //multipleGroup : true
         });
         });
      </script>
   </body>
</html>
`)
