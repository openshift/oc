package inspect

const eventHTMLPage = `
<!doctype html>
<html lang="en">

<head>
  <!-- Required meta tags -->
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1, shrink-to-fit=no">

  <!-- Bootstrap CSS -->
  <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.2.0-beta1/dist/css/bootstrap.min.css" rel="stylesheet"
    integrity="sha384-0evHe/X+R7YkIZDRvuzKMRqM+OrBnVFBL6DOitfPri4tjfHxaWutUpFmBp4vmVor" crossorigin="anonymous">
  <link rel="stylesheet" href="https://use.fontawesome.com/releases/v5.6.3/css/all.css"
    integrity="sha384-UHRtZLI+pbxtHCWp1t77Bi1L4ZtiqrqD80Kn4Z8NTSRyMA2Fd33n5dQ8lWUE00s/" crossorigin="anonymous">
  <link href="https://unpkg.com/bootstrap-table@1.20.2/dist/bootstrap-table.min.css" rel="stylesheet">

  <title>Events</title>
  <style type="text/css">
    body * {
      font-size: 12px !important;
    }

    .text-overflow() {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .truncated {
      display: inline-block;
      max-width: 200px;
      .text-overflow();
    }
  </style>
</head>

<body>

  <p>
    <button class="btn btn-primary" type="button" data-bs-toggle="collapse" data-bs-target="#filteringGuide"
      aria-expanded="false" aria-controls="filteringGuide">
      Filtering guide
    </button>
  </p>
  <div class="collapse" id="filteringGuide">
    <div class="card card-body">
      <h1>Time</h1>
      Time field recognizes two types of format:
      <ul>
        <li>datetime - date and time - seconds and whitespace optional</li>
        <ul>
          <li>YYYY-mm-DD HH:MM[:SS]</li>
          <li>YYYY-mm-DD<b>t</b>HH:MM[:SS] (t either lower or uppercase)</li>
          <li>YYYYmmDDHHMM[SS] </li>
        </ul>
        <li>time</li>
        <ul>
          <li>HH:MM[:SS]</li>
          <li>HHMM[SS]</li>
        </ul>
      </ul>
      <p>
        Formats can be used to search for a specific point in time or an interval.
        Datetime format cannot be mixed with time format in single filter.
        If seconds are not given an interval is assumed (for example: 20:00 means 20:00:00-20:00:59).
        Intervals without dates are not allowed for events spanning more than 24h.
      </p>
      <h2>Examples</h2>
      <ul>
        <li><em>2022-01-01 20:00 - 2022-01-01 21:00</em> - will search for event between 20:00:00 and 21:00:59 on
          2022-01-01</li>
        <li><em>2022-01-01 20:00</em> - will search for events between 20:00:00 and 20:00:59 on 2022-01-01</li>
        <li><em>20:00 - 21:00</em> - will search for event between 20:00:00 and 21:00:59</li>
        <li><em>20:00</em> - will search for events between 20:00:00 and 20:00:59</li>
        <li><em>20:00:05</em> - will search for events that happened exactly at 20:00:05</li>
      </ul>

      <h1>Rest of the fields</h1>
      Columns such as Namespace, Component, RelatedObject, and Reason support JS regular expressions.
      <h2>Examples</h2>
      <ul>
        <li><em>(def|kube-system)</em></li>
        <li><em>openshift.*registry</em></li>
        <li><em>^(?!kube).*</em> - filters out cells (like namespace) starting with kube</li>
      </ul>
    </div>
  </div>

  <div id="errorMsg" class="card card-body bg-warning" style="display: none"> </div>

  <table id="events" class="table table-bordered table-hover table-sm" data-toggle="table" data-search="true"
    data-regex-search="true" data-show-search-clear-button="true" data-filter-control="true"
    data-id-table="advancedTable" data-pagination="true" data-page-size="100" data-show-columns-toggle-all="true"
    data-show-pagination-switch="true" data-show-columns="true" data-addrbar="true">
    <thead>
      <tr>
        <th data-width="100" data-field="time" data-filter-control="input" data-sortable="true" data-filter-custom-search="timeSearch">Time</th>
        <th data-width="200" data-field="namespace" data-filter-control="input" data-sortable="true" data-filter-custom-search="textSearch">Namespace</th>
        <th data-width="200" data-field="component" data-filter-control="input" data-sortable="true" data-filter-custom-search="textSearch">Component</th>
        <th data-width="200" data-field="relatedobject" data-filter-control="input" data-sortable="true" data-filter-custom-search="textSearch">RelatedObject</th>
        <th data-field="reason" data-filter-control="input" data-filter-custom-search="textSearch">Reason</th>
        <th data-field="message" data-filter-control="input" data-escape="true">Message</th>
      </tr>
    </thead>
    <tbody>
      {{range .Items}}
      <tr>
        <td>{{formatTime .ObjectMeta.CreationTimestamp .FirstTimestamp .LastTimestamp .Count}}</td>
        <td>
          <p class="truncated">{{.Namespace}}</p>
        </td>
        <td>
          <p class="truncated">{{.Source.Component}}</p>
        </td>
        <td>
          <p class="truncated">{{.InvolvedObject.Name}}</p>
        </td>
        <td>{{formatReason .Reason}}</td>
        <td data-formatter="messageFormatter">{{.Message}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>

  <!-- Optional JavaScript -->
  <!-- jQuery first, then Popper.js, then Bootstrap JS -->
  <script src="https://code.jquery.com/jquery-3.3.1.min.js"
    integrity="sha256-FgpCb/KJQlLNfOu91ta32o/NMZxltwRo8QtmkMRdAu8=" crossorigin="anonymous"></script>
  <script src="https://cdnjs.cloudflare.com/ajax/libs/popper.js/1.14.7/umd/popper.min.js"
    integrity="sha384-UO2eT0CpHqdSJQ6hJty5KVphtPhzWj9WO1clHTMGa3JDZwrnQq4sF86dIHNDz0W1"
    crossorigin="anonymous"></script>
  <script src="https://cdn.jsdelivr.net/npm/bootstrap@5.2.0-beta1/dist/js/bootstrap.bundle.min.js"
    integrity="sha384-pprn3073KE6tl6bjs2QrFaJGz5/SUsLqktiwsUTF55Jfv3qYSDhgCecCxMW52nD2"
    crossorigin="anonymous"></script>

  <script src="https://unpkg.com/bootstrap-table@1.21.1/dist/bootstrap-table.min.js" crossorigin="anonymous"></script>
  <script src="https://unpkg.com/bootstrap-table@1.21.1/dist/extensions/toolbar/bootstrap-table-toolbar.min.js"
    crossorigin="anonymous"></script>
  <script
    src="https://unpkg.com/bootstrap-table@1.21.1/dist/extensions/filter-control/bootstrap-table-filter-control.min.js"
    crossorigin="anonymous"></script>
  <script src="https://cdn.jsdelivr.net/npm/luxon@2.4.0/build/global/luxon.min.js" crossorigin="anonymous"></script>

  <script>
    'use strict';

    function messageFormatter(value, row) {
      return '<code>' + value + '</code>'
    }
    $(function () {
      $('#events').bootstrapTable()
    })

    // YYYY-mm-DD HH:MM[:SS]
    // YYYY-mm-DDtHH:MM[:SS] (t either lower or uppercase)
    const dateTimeRegexp = /(?<year>\d{4})-?(?<month>\d{2})-?(?<day>\d{2})[\s*|Tt]?(?<hour>\d{2}):?(?<minute>\d{2}):?(?<second>\d{2})?/

    // YYYYmmDDHHMM[SS]
    const dateTimeNoWhitespaceRegexp = /(?<year>\d{4})(?<month>\d{2})(?<day>\d{2})(?<hour>\d{2})(?<minute>\d{2}):?(?<second>\d{2})?/

    // HH:MM[:SS] and HHMM[SS]
    const timeRegexp = /(?<hour>\d{2}):?(?<minute>\d{2}):?(?<second>\d{2})?/

    const parser = new DOMParser()
    function getDateTimeFromTag(tag) {
      return luxon.DateTime.fromISO(parser.
        parseFromString(tag, 'text/html').getElementsByTagName('time')[0].
        dateTime.split(' ', 2).join('T'))
    }

    var firstEvent = null
    var lastEvent = null
    function findFirstAndLastEvents(allData) {
      if (firstEvent !== null && lastEvent !== null) {
        return
      }

      allData.forEach(e => {
        var dt = getDateTimeFromTag(e.time)

        if (firstEvent === null || dt < firstEvent) {
          firstEvent = dt
        }

        if (lastEvent === null || lastEvent < dt) {
          lastEvent = dt
        }

      });
    }

    var lastErrMsgQuery = ""
    function setErrorMsg(msg, query) {
      if (lastErrMsgQuery !== query) {
        $("#errorMsg").text(msg).show()
        lastErrMsgQuery = query
      }
    }

    function textSearch(query, cellToTest, _columnName, allData) {
      const cellContent = ((cell) => {
        if (cell.startsWith("<p")) {
          var p = document.createElement('p')
          p.innerHTML = cell
          return p.textContent
        } else {
          return cell
        }
      })(cellToTest)
      return (new RegExp(query, 'i')).test(cellContent)
    }

    function timeSearch(query, cellToTest, _columnName, allData) {
      if (lastErrMsgQuery !== query) {
        $("#errorMsg").hide()
        lastErrMsgQuery = ""
      }

      const cellDT = getDateTimeFromTag(cellToTest)

      for (const rg of [dateTimeRegexp, dateTimeNoWhitespaceRegexp, timeRegexp]) {
        if (rg.test(query)) {
          return timeSearchCommon(new RegExp(rg, 'g'), query, cellDT, allData)
        }
      }

      setErrorMsg("Unrecognized filter - refer to guide", query)
      return false
    }

    function timeSearchCommon(rg, query, cellDT, allData) {
      const matches = Array.from(query.matchAll(rg))

      if (matches.length !== 1 && matches.length !== 2) {
        setErrorMsg(["Expected 1 or 2 (date)times but found", matches.length, ": ", matches.map(e => JSON.stringify(e.groups))].join(' '), query)
        return false
      }

      const dateIsPresent = typeof matches[0].groups.year !== "undefined"
      findFirstAndLastEvents(allData)
      if (!dateIsPresent && luxon.Interval.fromDateTimes(firstEvent, lastEvent).length("hours") > 24) {
        setErrorMsg(["Events span more than 24 hours - use interval with dates, for example:", firstEvent.toFormat('yyyy-MM-dd HH:mm:ss'), " - ", lastEvent.toFormat('yyyy-MM-dd HH:mm:ss')].join(" "))
        return false
      }

      const check = function (dateFrom, timeFrom, dateTo, timeTo) {
        const f = luxon.DateTime.fromObject({
          year: dateFrom.year,
          month: dateFrom.month,
          day: dateFrom.day,
          hour: timeFrom.hour,
          minute: timeFrom.minute,
          second: typeof timeFrom.second === "undefined" ? 0 : timeFrom.second,
        })

        var t = luxon.DateTime.fromObject({
          year: dateTo.year,
          month: dateTo.month,
          day: dateTo.day,
          hour: timeTo.hour,
          minute: timeTo.minute,
          second: typeof timeTo.second === "undefined" ? 59 : timeTo.second,
        })

        return f <= cellDT && cellDT <= t
      }

      const indexFrom = 0, indexTo = matches.length - 1 // indexTo will be 0 for single time point query (HHmm) and 1 for time interval query (HHmm - HHmm)
      const eventsSpanSingleDay = firstEvent.day === lastEvent.day
      if (eventsSpanSingleDay) {
        return check(cellDT, matches[indexFrom].groups, cellDT, matches[indexTo].groups)
      }

      if (dateIsPresent) {
        return check(matches[indexFrom].groups, matches[indexFrom].groups, matches[indexTo].groups, matches[indexTo].groups)
      }

      const dateFrom = matches[indexFrom].groups.hour >= firstEvent.hour ? firstEvent : lastEvent
      const dateTo = matches[indexTo].groups.hour >= firstEvent.hour ? firstEvent : lastEvent
      return check(dateFrom, matches[indexFrom].groups, dateTo, matches[indexTo].groups)
    }

    ///////////////////////////////////////////////////////////////////////////////////////////////////////////
    // Below is modified code from: https://github.com/wenzhixin/bootstrap-table/blob/develop/src/extensions/addrbar/bootstrap-table-addrbar.js
    // Original author: generals.space@gmail.com
    // Licence: MIT(https://github.com/wenzhixin/bootstrap-table/blob/develop/LICENSE)
    function _GET(key, url = window.location.hash) {
      const reg = new RegExp("(^|&)" + key + "=([^&]*)(&|$)")
      const result = url.substr(1).match(reg)

      if (result) {
        return decodeURIComponent(result[2])
      }
      return null
    }

    function _buildUrl(dict, url = window.location.hash) {
      for (const [key, val] of Object.entries(dict)) {
        const pattern = key + "=([^&]*)"
        const targetStr = key + "=" + val

        if (val === undefined) {
          continue
        }

        if (url.match(pattern)) {
          const tmp = new RegExp("(" + key + "=)([^&]*)", 'gi')
          url = url.replace(tmp, targetStr)
        } else {
          url = url + targetStr + '&'
        }
      }
      
      return url.slice(-1) === '&' ? url.slice(0, -1) : url
    }

    function updateURL(table, _prefix) {
      const params = {}

      params[_prefix + "page"] = table.options.pageNumber
      params[_prefix + "size"] = table.options.pageSize
      params[_prefix + "search"] = table.options.searchText

      params[_prefix + "order"] = table.options.sortOrder
      params[_prefix + "sort"] = table.options.sortName;

      ["time", "namespace", "component", "relatedobject", "reason", "message"].forEach(columnName => {
        params[_prefix + columnName] = table._valuesFilterControl.find(column => column.field === columnName).value
      });

      window.location.hash = _buildUrl(params)
    }

    $.extend($.fn.bootstrapTable.defaults, {
      addrbar: false,
      addrPrefix: ''
    })

    $.BootstrapTable = class extends $.BootstrapTable {
      init(...args) {
        if (this.options.pagination && this.options.addrbar) {
          this.addrbarInit = true

          this.options.pageNumber = +this.getDefaultOptionValue('pageNumber', 'page')
          this.options.pageSize = +this.getDefaultOptionValue('pageSize', 'size')
          this.options.sortOrder = this.getDefaultOptionValue('sortOrder', 'order')
          this.options.sortName = this.getDefaultOptionValue('sortName', 'sort')
          this.options.searchText = this.getDefaultOptionValue('searchText', 'search');

          ["time", "namespace", "component", "relatedobject", "reason", "message"].forEach(columnName => {
            $("th[data-field=\"" + columnName + "\"]").attr("data-filter-default", _GET((this.options.addrPrefix ? this.options.addrPrefix : '') + columnName))
          });

          const _prefix = this.options.addrPrefix || ''
          const _onLoadSuccess = this.options.onLoadSuccess
          const _onPageChange = this.options.onPageChange
          const _onSort = this.options.onSort

          this.options.onLoadSuccess = data => {
            if (this.addrbarInit) {
              this.addrbarInit = false
            } else {
              updateURL(this, _prefix)
            }

            if (_onLoadSuccess) {
              _onLoadSuccess.call(this, data)
            }
          }

          this.options.onPageChange = (number, size) => {
            updateURL(this, _prefix)

            if (_onPageChange) {
              _onPageChange.call(this, number, size)
            }
          }

          this.options.onSort = (name, order) => {
            updateURL(this, _prefix)

            if (_onSort) {
              _onSort.call(this, name, order)
            }
          }
        }
        super.init(...args)
      }

      getDefaultOptionValue(optionName, prefixName) {
        if (this.options[optionName] !== $.BootstrapTable.DEFAULTS[optionName]) {
          return this.options[optionName]
        }
        return _GET(this.options.addrPrefix ? this.options.addrPrefix : '' + prefixName) || $.BootstrapTable.DEFAULTS[optionName]
      }
    }
    ///////////////////////////////////////////////////////////////////////////////////////////////////////////
  </script>
</body>

</html>
`
